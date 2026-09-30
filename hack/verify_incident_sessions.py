#!/usr/bin/env python3
"""Bounded live walkthrough against an explicitly configured test deployment.

Requires two different, equally authorised identities in one namespace and a
running fixture Pod. Creates at most one session per identity; never retries a
mutation. Does not provision infrastructure or change permissions.
"""

import argparse
import base64
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
import time


NOTE = "synthetic incident-session verification note"


def require(condition, description):
    if not condition:
        raise RuntimeError(description)


class Walkthrough:
    def __init__(self, options):
        self.options = options
        self.last_request = 0
        self.sessions = {}
        self.checks = []
        self.output = Path(options.output)
        self.output.mkdir(mode=0o700, parents=False, exist_ok=False)
        self.save("annotation.txt", NOTE + "\n")

    def save(self, name, text):
        fd = os.open(self.output / name, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
        with os.fdopen(fd, "w") as stream:
            stream.write(text)

    def command(self, actor, arguments, *, expect_error=None):
        # Respect the namespace's two-request/second admission rate.
        time.sleep(max(0, 0.6 - (time.monotonic() - self.last_request)))
        self.last_request = time.monotonic()
        config = getattr(self.options, actor + "_kubeconfig")
        context = getattr(self.options, actor + "_context")
        args = [self.options.binary, "--kubeconfig", config, "--context", context,
                "--connect-mode", "kubernetes-api", "session", "-n",
                self.options.namespace, *arguments]
        try:
            result = subprocess.run(args, capture_output=True, text=True, timeout=20)
        except (OSError, subprocess.TimeoutExpired):
            raise RuntimeError("command failed or timed out; mutation was not retried") from None
        if expect_error is not None:
            require(result.returncode != 0 and not result.stdout
                    and result.stderr.strip() in expect_error,
                    "expected API rejection was not confirmed")
        else:
            require(result.returncode == 0, "command failed; mutation was not retried")
        return result.stdout

    def check(self, description, condition):
        require(condition, description)
        self.checks.append(description)
        print("PASS", description, flush=True)

    def export(self, actor, session, *, sensitive=False):
        args = ["export", session]
        if sensitive:
            args.append("--include-sensitive")
        raw = self.command(actor, args)
        return raw, json.loads(raw)

    def run(self):
        for actor in ("owner", "peer"):
            started = json.loads(self.command(actor, ["start", "-o", "json"]))
            self.sessions[actor] = started["id"]
            self.check("explicit creation for " + actor, started["latest"]["kind"] == "opened")
            # Both accounts must have working export permission before isolation
            # can count as evidence, rather than expired credentials or missing RBAC.
            self.export(actor, started["id"], sensitive=True)
        session = self.sessions["owner"]
        self.command("peer", ["export", session, "--include-sensitive"],
                     expect_error={"incident session access denied",
                                   "incident session or evidence is unavailable"})
        self.check("different authorised identity cannot export owner's session", True)
        self.command("owner", ["annotate", session, "--note-file",
                               str(self.output / "annotation.txt")])
        public_raw, public = self.export("owner", session)
        _, full = self.export("owner", session, sensitive=True)
        self.check("sanitised export removes annotation and namespace",
                   public["redacted"] and NOTE not in public_raw
                   and self.options.namespace not in public_raw)
        self.check("authorised export retains annotation",
                   not full["redacted"] and any(e.get("note") == NOTE for e in full["entries"]))
        self.command("owner", ["capture", session, self.options.pod])
        _, full = self.export("owner", session, sensitive=True)
        self.check("real Pod capture retains bytes",
                   any(e["kind"] == "captured" for e in full["entries"])
                   and len(full.get("captures", [])) == 1)
        capture = json.loads(base64.b64decode(full["captures"][0]["data"], validate=True))
        self.check("capture identifies requested Pod",
                   capture["pods"][0]["podName"] == self.options.pod
                   and capture["pods"][0]["namespace"] == self.options.namespace)
        self.command("owner", ["compare", session, "evidence-1", "evidence-1"])
        summary = json.loads(self.command("owner", ["status", session, "-o", "json"]))
        self.check("retained evidence comparison recorded", summary["latest"]["kind"] == "compared")
        self.markers(session)
        if self.options.trace_report:
            self.trace_reference(session)
        self.command("owner", ["close", session])
        self.command("owner", ["annotate", session, "--note-file",
                               str(self.output / "annotation.txt")],
                     expect_error={"incident session is closed"})
        self.check("closed session rejects annotation", True)
        full_raw, _ = self.export("owner", session, sensitive=True)
        public_raw, public = self.export("owner", session)
        self.check("public export has no retained bytes or private annotation",
                   "captures" not in public and NOTE not in public_raw)
        self.save("authorised.private.json", full_raw)
        self.save("sanitised.json", public_raw)
        if self.options.trace_report:
            self.verify_trace()
        replay = subprocess.run([self.options.binary, "session", "replay",
                                 str(self.output / "authorised.private.json")],
                                capture_output=True, text=True, timeout=20)
        self.check("offline replay omits private annotation",
                   replay.returncode == 0 and "Recorded session" in replay.stdout
                   and NOTE not in replay.stdout)

    def trace_reference(self, session):
        path = Path(self.options.trace_report)
        require(path.is_file() and not path.is_symlink() and path.stat().st_size <= 32768,
                "trace report must be a bounded regular file")
        data = path.read_bytes()
        require(len(data) <= 32768, "trace report exceeded the size bound")
        self.command("owner", ["trace-reference", session, "--report", str(path),
                               "--confirm-reference"])
        _, full = self.export("owner", session, sensitive=True)
        public_raw, public = self.export("owner", session)
        private_ref = full["entries"][-1]["traceReference"]
        public_ref = public["entries"][-1]["traceReference"]
        self.check("trace reference binds exact local bytes with operator provenance",
                   full["schemaVersion"] == 2
                   and private_ref["digest"] == hashlib.sha256(data).hexdigest()
                   and private_ref["bytes"] == len(data)
                   and private_ref["provenance"] == "operator-supplied")
        allowed = {"alias", "provenance", "reportSchemaVersion", "traceKind", "state",
                   "cleanup", "transportComplete", "termination", "coverage", "loss"}
        self.check("sanitised trace keeps outcomes without hashes or counters",
                   public["schemaVersion"] == 2 and set(public_ref) == allowed
                   and public_ref["alias"] == "trace-1"
                   and public_ref["provenance"] == "operator-supplied"
                   and private_ref["digest"] not in public_raw
                   and all(public_ref[key] == private_ref[key] for key in
                           ("traceKind", "state", "cleanup", "transportComplete",
                            "termination", "coverage")))
        self.command("peer", ["trace-reference", session, "--report", str(path),
                              "--confirm-reference"],
                     expect_error={"incident session access denied",
                                   "incident session or evidence is unavailable"})
        self.check("different authorised identity cannot attach to owner's timeline", True)

    def verify_trace(self):
        args = [self.options.binary, "session", "verify-trace",
                str(self.output / "authorised.private.json"), "trace-1", "--report"]
        result = subprocess.run([*args, self.options.trace_report],
                                capture_output=True, text=True, timeout=20)
        self.check("offline verification matches bytes without claiming authenticity",
                   result.returncode == 0 and result.stdout.strip() ==
                   "Report bytes match the incident reference. Source authenticity remains unverified.")
        # Whitespace preserves valid JSON but must change the exact-byte binding.
        changed = Path(self.options.trace_report).read_text() + "\n"
        self.save("changed-report.private.json", changed)
        result = subprocess.run([*args, str(self.output / "changed-report.private.json")],
                                capture_output=True, text=True, timeout=20)
        self.check("offline verification rejects a changed report",
                   result.returncode != 0 and not result.stdout
                   and result.stderr.strip() == "trace report does not match the selected incident reference")

    def markers(self, session):
        self.command("owner", ["markers", session, self.options.pod, "--source", "local"])
        _, full = self.export("owner", session, sensitive=True)
        if self.options.markers == "disabled":
            self.check("disabled marker source is an explicit gap",
                       full["entries"][-1].get("gapReason") == "source-disabled")
            return
        retained = [c for c in full.get("captures", []) if c["schemaVersion"] == 6]
        self.check("local history and markers retained",
                   len(retained) == 1 and any(e["kind"] == "marked" for e in full["entries"]))
        evidence = json.loads(base64.b64decode(retained[0]["data"], validate=True))["context"]
        self.check("marker capture preserves local source and Kubernetes markers",
                   evidence["history"]["query"]["source"] == "local"
                   and len(evidence["changes"]["markers"]) > 0)

    def cleanup(self):
        failures = []
        for actor, session in self.sessions.items():
            try:
                self.command(actor, ["delete", session])
                self.command(actor, ["status", session],
                             expect_error={"incident session or evidence is unavailable"})
                self.check("owned session deleted for " + actor, True)
            except RuntimeError:
                failures.append(actor)
        require(not failures, "session cleanup could not be confirmed for " + ", ".join(failures))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    for flag in ("binary", "owner-kubeconfig", "owner-context", "peer-kubeconfig",
                 "peer-context", "namespace", "pod", "output"):
        parser.add_argument("--" + flag, required=True)
    parser.add_argument("--markers", choices=("disabled", "local"), required=True)
    parser.add_argument("--trace-report", help="existing local report for the explicit reference walkthrough")
    options = parser.parse_args()
    walkthrough = Walkthrough(options)
    failures = []
    try:
        walkthrough.run()
    except (RuntimeError, ValueError, KeyError, IndexError, OSError, subprocess.TimeoutExpired):
        failures.append("walkthrough failed; review the last passing check")
    try:
        walkthrough.cleanup()
    except RuntimeError as error:
        failures.append(str(error))
    walkthrough.save("results.json", json.dumps({"passed": not failures,
                     "checks": walkthrough.checks, "failures": failures}, indent=2) + "\n")
    for failure in failures:
        print(failure, file=sys.stderr)
    return bool(failures)


if __name__ == "__main__":
    sys.exit(main())
