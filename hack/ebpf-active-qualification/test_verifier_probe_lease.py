from pathlib import Path
import tempfile
import unittest

from verifier_probe_lease import ProbeLease, REGISTRY, definitions

OWNER = 'a' * 32
HELPER = '/usr/local/bin/kml-active-verifier-1'


class Runtime:
    def __init__(self):
        self.lines = ['p:unrelated/keep bpf_check']
        self.writes = []
        self.alive = False

    def exec(self, args):
        if args == ['cat', REGISTRY]:
            return ('\n'.join(self.lines) + '\n').encode()
        if args[-1] == HELPER:
            if self.alive:
                raise RuntimeError('native helper still alive')
            return b''
        name = args[-1]
        if not name.startswith('-:') or args[:2] != ['sh', '-ec']:
            raise AssertionError('unexpected runtime mutation')
        self.writes.append(name[2:])
        self.lines = [line for line in self.lines if line.split()[0].split(':', 1)[1] != name[2:]]
        return b''


class ProbeLeaseTests(unittest.TestCase):
    def test_journal_precedes_capture_and_cleanup_preserves_other_owners(self):
        runtime = Runtime()
        with tempfile.TemporaryDirectory() as temp:
            lease = ProbeLease(runtime, Path(temp), OWNER, HELPER)
            self.assertTrue((Path(temp) / 'verifier-probes.private.json').exists())
            self.assertFalse(runtime.writes)
            runtime.lines.extend(definitions(OWNER).values())
            lease.close(); lease.close()
            self.assertEqual(runtime.writes, list(reversed(list(definitions(OWNER)))))
            self.assertEqual(runtime.lines, ['p:unrelated/keep bpf_check'])

    def test_preexisting_owner_is_never_adopted(self):
        runtime = Runtime(); runtime.lines.extend(definitions(OWNER).values())
        with tempfile.TemporaryDirectory() as temp:
            with self.assertRaisesRegex(ValueError, 'already'):
                ProbeLease(runtime, Path(temp), OWNER, HELPER)
        self.assertFalse(runtime.writes)

    def test_live_native_helper_prevents_reconciliation(self):
        runtime = Runtime()
        with tempfile.TemporaryDirectory() as temp:
            lease = ProbeLease(runtime, Path(temp), OWNER, HELPER)
            runtime.lines.extend(definitions(OWNER).values()); runtime.alive = True
            with self.assertRaises(RuntimeError): lease.close()
            self.assertFalse(runtime.writes)
            runtime.alive = False
            lease.close()
            self.assertEqual(len(runtime.writes), 3)

    def test_changed_unknown_or_duplicate_owned_definition_refuses_all_deletion(self):
        for change in ('target', 'unknown', 'duplicate'):
            with self.subTest(change=change), tempfile.TemporaryDirectory() as temp:
                runtime = Runtime(); lease = ProbeLease(runtime, Path(temp), OWNER, HELPER)
                runtime.lines.extend(definitions(OWNER).values())
                if change == 'target': runtime.lines[-1] += ' bad=+0($arg1):u64'
                elif change == 'unknown': runtime.lines.append('p:kml_verifier_' + OWNER + '/unknown bpf_check')
                else: runtime.lines.append(runtime.lines[-1])
                with self.assertRaises(ValueError): lease.close()
                self.assertFalse(runtime.writes)

    def test_partial_registration_and_already_closed_native_capture(self):
        for count in (0, 1, 2):
            with self.subTest(count=count), tempfile.TemporaryDirectory() as temp:
                runtime = Runtime(); lease = ProbeLease(runtime, Path(temp), OWNER, HELPER)
                runtime.lines.extend(list(definitions(OWNER).values())[:count])
                lease.close()
                self.assertEqual(len(runtime.writes), count)
                self.assertEqual(runtime.lines, ['p:unrelated/keep bpf_check'])


if __name__ == '__main__':
    unittest.main()
