"""Verify the test-only stream engine through owned local Kubernetes aggregation.

Uses the documented tenant fixtures. Temporarily removes the fixture stream Role
permission and restores it in finally. No event content enters the result file.
"""
import argparse
from contextlib import contextmanager
import http.client
import json
import os
from pathlib import Path
import subprocess

from admission_api import A, INTENT, Session


class Stream:
    def __init__(self, response):
        self.response = response
        self.frames = []
        self.size = 0
        self.before_summary = 0

    def next(self):
        line = self.response.readline(8193)
        if not line:
            return None
        if len(line) > 8192 or not line.endswith(b"\n"):
            raise AssertionError("invalid frame size or delimiter")
        frame = json.loads(line)
        if frame["version"] != 1:
            raise AssertionError("unexpected frame version")
        self.before_summary = self.size
        self.size += len(line)
        if self.size > 8 * 1024 * 1024:
            raise AssertionError("stream exceeded output ceiling")
        self.frames.append(frame)
        return frame

    def drain(self):
        while self.next() is not None:
            pass
        types = [frame["type"] for frame in self.frames]
        if types != ["metadata"] + ["event"] * (len(types) - 2) + ["summary"]:
            raise AssertionError("invalid stream ordering or missing terminal frame")
        summary = self.frames[-1]["summary"]
        if summary["writtenEvents"] != types.count("event") or summary["writtenBytesBeforeSummary"] != self.before_summary:
            raise AssertionError("incorrect delivery accounting")
        return summary


@contextmanager
def opened(session, duration=3, **limits):
    intent = dict(INTENT, durationSeconds=duration, **limits)
    result = session.expect("admission", session.call("a", "POST", A, intent), 201)
    path = A + "/" + result["metadata"]["name"]
    connection = http.client.HTTPSConnection(session.endpoint.hostname,
                                            session.endpoint.port, context=session.tls,
                                            timeout=duration + 6)
    try:
        connection.request("GET", path + "/stream", headers={
            "Authorization": "Bearer " + session.tokens["a"]})
        response = connection.getresponse()
        if response.status != 200:
            raise AssertionError(f"stream returned HTTP {response.status}")
        yield Stream(response), path
    finally:
        connection.close()
        status, _ = session.call("a", "DELETE", path)
        if status not in (200, 404, 410):
            raise AssertionError("fixture cleanup unconfirmed")


def passed(session, name):
    session.checks.append({"check": name, "outcome": "passed"})


def verify(session):
    with opened(session, maxEvents=3) as (stream, _):
        summary = stream.drain()
        if summary["termination"] != "event_limit" or summary["writtenEvents"] != 3:
            raise AssertionError("event ceiling was not enforced")
        if any("path" in f.get("event", {}).get("file", {}) for f in stream.frames):
            raise AssertionError("default stream disclosed a path")
    passed(session, "bounded events and bytes with default paths absent")

    with opened(session, duration=12) as (stream, _):
        if stream.drain()["termination"] != "expired":
            raise AssertionError("long stream did not expire normally")
    passed(session, "12-second stream crosses ordinary request deadlines")

    with opened(session, duration=8) as (stream, path):
        if stream.next()["type"] != "metadata":
            raise AssertionError("metadata missing")
        session.expect("other owner attachment denied", session.call("colleague", "GET", path + "/stream"), 404)
        session.expect("other owner cancellation denied", session.call("colleague", "DELETE", path), 404)
        session.expect("second consumer denied", session.call("a", "GET", path + "/stream"), 429)
        session.expect("owner cancellation", session.call("a", "DELETE", path), 200)
        if stream.drain()["termination"] != "cancelled":
            raise AssertionError("cancellation not reported")
    passed(session, "one owner and consumer with explicit cancellation")

    with opened(session, rawPaths=True, maxEvents=2) as (stream, _):
        stream.drain()
        if stream.frames[0]["metadata"]["paths"] != "confirmed":
            raise AssertionError("consent metadata differs")
        paths = [f["event"]["file"].get("path", "") for f in stream.frames if f["type"] == "event"]
        if not paths or not all("CONTRACT-FIXTURE-ONLY" in p and "\x1b" not in p and "\\u{001b}" in p for p in paths):
            raise AssertionError("fixture paths did not stay visibly escaped")
    passed(session, "confirmed paths remain escaped after JSON decoding")

    with opened(session, duration=8) as (stream, _):
        stream.next()
        command = session.command + ["-n", "trace-target-a"]
        role = json.loads(subprocess.check_output(command + ["get", "role", "trace-test", "-o", "json"]))
        role["metadata"] = {"name": "trace-test", "namespace": "trace-target-a"}
        restricted = dict(role, rules=[rule for rule in role["rules"] if "traces/stream" not in rule["resources"]])
        try:
            apply_role(command, restricted)
            summary = stream.drain()
            if summary["termination"] != "authorisation_lost" or not summary["incomplete"] or any(v is not None for v in summary["engineCounts"].values()):
                raise AssertionError("revocation did not suppress final engine observations")
        finally:
            apply_role(command, role)
    passed(session, "revocation ends stream with unknown final observations")


def apply_role(command, role):
    subprocess.run(command + ["apply", "-f", "-"], input=json.dumps(role).encode(),
                   stdout=subprocess.DEVNULL, check=True, timeout=15)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--kubeconfig", required=True)
    parser.add_argument("--output", required=True)
    parser.add_argument("--owned-fixture", required=True, choices=["r6-stream"])
    args = parser.parse_args()
    os.umask(0o077)
    session = Session(args.kubeconfig, "kind-kube-memlens-node-context-r6-stream")
    verify(session)
    Path(args.output).write_text(json.dumps({
        "executionModel": "test-only memory engine; real aggregation, TLS, SAR and cgroup binding",
        "incidentProgrammeLoaded": False, "checks": session.checks,
    }, indent=2) + "\n")
    print(f"Passed {len(session.checks)} live stream checks.")
