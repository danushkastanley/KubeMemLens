"""Bound local observer processes and private Node inputs to one window."""
import json
import re
import subprocess
import time

from local_case import canonical, digest


class Processes:
    def __init__(self, case, directory, stem):
        self.case, self.directory, self.stem = case, directory, stem
        self.items = []
        self.private = {}
        self.budget_failures = set()
        self.scheduler_completion = None

    def configuration(self, name, value):
        raw = canonical(value)
        path = '/tmp/kml-active-' + self.stem + '-' + name + '-' + digest(raw)[:12] + '.json'
        self.case.runtime.exec(['sh', '-ec', 'umask 077; set -C; cat > "$1"', '--', path], raw)
        self.private[path] = digest(raw)
        return path

    def start(self, name, args, stdin=None):
        if not re.fullmatch(r'[a-z][a-z0-9-]{0,79}', name):
            raise ValueError('invalid observation process label')
        out = (self.directory / (name + '.jsonl')).open('xb')
        err = (self.directory / (name + '.stderr')).open('xb')
        try:
            process = subprocess.Popen(args, stdin=stdin, stdout=out, stderr=err)
        except BaseException:
            out.close()
            err.close()
            raise
        self.items.append((name, process, out, err))
        return process

    def native(self, name, helper, *args):
        return self.start(name, ['docker', 'exec', self.case.node, self.case.cfg['helpers'][helper]['path'], *args])

    def scheduler(self, config_path):
        args = [self.case.cfg['helpers']['scheduler']['path'], '--config', config_path,
                '--acknowledge-owned-node', '--completion-signal', 'stdin-eof']
        process = self.start('scheduler', self.case.runtime.observer_input_command(args), stdin=subprocess.PIPE)
        self.scheduler_completion = process.stdin
        return process

    def release_scheduler(self, states):
        if self.scheduler_completion is None:
            return
        peers = {name: code for name, _, code in states}
        if 'verifier' not in peers:
            raise ValueError('scheduler completion requires a verifier process')
        if peers['scheduler'] is not None:
            self.record_failure([('scheduler', peers['scheduler'])])
            raise ValueError('scheduler exited before verifier completion acknowledgement')
        if peers['verifier'] == 0:
            self.scheduler_completion.close()
            self.scheduler_completion = None

    def wait(self, process, seconds):
        if process.wait(timeout=seconds) != 0:
            raise ValueError('observer or workload failed; partial records retained')

    def accept_delivery_budget_failure(self, process):
        if not any(p is process and name.startswith('delivery-') for name, p, _, _ in self.items) or process.returncode != 1:
            raise ValueError('only a verified complete delivery failure may be retained')
        self.budget_failures.add(process)

    def healthy(self):
        for name, process, _, _ in self.items:
            code = process.poll()
            if code is not None and ((code != 0 and process not in self.budget_failures) or name in ('resources', 'standard', 'witness', 'scheduler', 'verifier')):
                self.record_failure([(name, code)])
                raise ValueError('required observer stopped before its window ended')

    def record_failure(self, failed):
        with (self.directory / 'failed-processes.private.json').open('x') as stream:
            json.dump({'schemaVersion': 1, 'controllerMonotonicNanos': time.monotonic_ns(),
                       'processes': [{'label': name, 'exitCode': code} for name, code in failed]}, stream)

    def wait_all(self, deadline):
        while True:
            states = [(name, process, process.poll()) for name, process, _, _ in self.items]
            codes = [code for _, _, code in states]
            failed = [(name, code) for name, process, code in states
                      if code is not None and code != 0 and process not in self.budget_failures]
            if failed:
                self.record_failure(failed)
                raise ValueError('observer or workload failed; retain partial records')
            self.release_scheduler(states)
            if all(code is not None for code in codes):
                return
            if time.monotonic() >= deadline:
                raise ValueError('observer window exceeded its deadline')
            time.sleep(.25)

    def close(self):
        failures = []
        if self.scheduler_completion is not None:
            try:
                self.scheduler_completion.close()
                self.scheduler_completion = None
            except Exception:
                failures.append('scheduler completion pipe')
        for name, process, out, err in self.items:
            try:
                if process.poll() is None:
                    process.wait(timeout=45)
            except subprocess.TimeoutExpired:
                # This only stops this controller's Docker/kubectl client. The
                # native helpers retain their own hard lifetimes; teardown of the
                # owned fixture/service invalidates their pinned process inputs.
                process.terminate()
                try:
                    process.wait(timeout=5)
                except subprocess.TimeoutExpired:
                    process.kill()
                    process.wait(timeout=5)
                failures.append(name)
            finally:
                out.close()
                err.close()
        for path, expected in self.private.items():
            try:
                if self.case.runtime.exec(['sha256sum', path]).decode().split()[0] != expected:
                    raise ValueError('private configuration changed')
                self.case.runtime.exec(['rm', '--', path])
            except Exception:
                failures.append('private configuration')
        if failures:
            raise ValueError('window process/configuration cleanup incomplete')


def read_document(path, maximum=2 << 20):
    if path.stat().st_size > maximum:
        raise ValueError('observer document exceeds bound')
    with path.open() as stream:
        value = json.load(stream)
    return value


def wait_until(deadline):
    while time.monotonic() < deadline:
        time.sleep(max(0, min(.25, deadline - time.monotonic())))
