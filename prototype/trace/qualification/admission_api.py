"""Exercise admission through an owned local kind cluster, using real tenant tokens.

Requires the documented trace-target-a/b fixtures and their tenant/colleague
ServiceAccounts. Reports contain assertions and HTTP statuses, not identities.
"""
import argparse
import base64
import http.client
import json
import ssl
import subprocess
import urllib.parse
from pathlib import Path


class Session:
    def __init__(self, kubeconfig, context="kind-kml-r6-admission"):
        self.command = ["kubectl", "--kubeconfig", kubeconfig]
        config = json.loads(subprocess.check_output(
            self.command + ["config", "view", "--minify", "--raw", "-o", "json"]))
        if context not in ("kind-kml-r6-admission", "kind-kube-memlens-node-context-r6-stream") or config["current-context"] != context:
            raise ValueError("requires the exact owned qualification fixture cluster")
        cluster = config["clusters"][0]["cluster"]
        self.endpoint = urllib.parse.urlsplit(cluster["server"])
        if self.endpoint.scheme != "https" or self.endpoint.hostname not in ("127.0.0.1", "localhost"):
            raise ValueError("qualification requires the local kind endpoint")
        self.tls = ssl.create_default_context(cadata=base64.b64decode(
            cluster["certificate-authority-data"]).decode())
        self.tokens = {}
        for key, namespace, name in (
            ("a", "trace-target-a", "tenant"), ("b", "trace-target-b", "tenant"),
            ("colleague", "trace-target-a", "colleague"),
        ):
            self.tokens[key] = subprocess.check_output(self.command + [
                "-n", namespace, "create", "token", name, "--duration=10m"
            ], text=True).strip()
        self.checks = []

    def call(self, who, method, path, body=None):
        connection = http.client.HTTPSConnection(self.endpoint.hostname,
                                                self.endpoint.port, context=self.tls, timeout=15)
        headers = {"Authorization": "Bearer " + self.tokens[who]}
        if body is not None:
            headers["Content-Type"] = "application/json"
            body = json.dumps(body).encode()
        try:
            connection.request(method, path, body, headers)
            response = connection.getresponse()
            data = response.read(65537)
            if len(data) > 65536:
                raise ValueError("response exceeds qualification limit")
            return response.status, json.loads(data) if data else None
        finally:
            connection.close()

    def expect(self, name, result, status):
        actual, data = result
        if actual != status:
            raise AssertionError(f"{name}: expected HTTP {status}, received {actual}")
        self.checks.append({"check": name, "status": actual, "outcome": "passed"})
        return data

    def save(self, output):
        Path(output).write_text(json.dumps({
            "scope": "local Kubernetes aggregation, tenant policy and exact node binding",
            "checks": self.checks,
        }, indent=2) + "\n")


BASE = "/apis/tracing.kubememlens.io/v1alpha1/namespaces/"
A = BASE + "trace-target-a/traces"
B = BASE + "trace-target-b/traces"
INTENT = {"schemaVersion": 1, "pod": "target", "container": "worker", "kind": "files"}


def verify(session):
    call, expect = session.call, session.expect
    active = []
    try:
        expect("cross namespace create denied", call("a", "POST", B, INTENT), 403)
        expect("runtime identity rejected", call("a", "POST", A, dict(INTENT, nodeUID="arbitrary")), 400)
        result = expect("authorised admission", call("a", "POST", A, INTENT), 201)
        path = A + "/" + result["metadata"]["name"]
        active.append(("a", path))
        if result["state"] != "admitted" or any(key in json.dumps(result) for key in
                                              ("containerID", "cgroupID", "podUID", "nodeUID")):
            raise AssertionError("admission response violated the private identity boundary")
        expect("owner revalidation", call("a", "GET", path), 200)
        expect("same namespace owner isolation", call("colleague", "GET", path), 404)
        expect("cross tenant read denied", call("b", "GET", path), 403)
        expect("node quota", call("b", "POST", B, INTENT), 429)
        expect("principal quota", call("a", "POST", A, INTENT), 429)
        expect("owner cancellation", call("a", "DELETE", path), 200)
        active.remove(("a", path))
        expect("cancelled admission absent", call("a", "GET", path), 404)
        result = expect("node quota released", call("b", "POST", B, INTENT), 201)
        path = B + "/" + result["metadata"]["name"]
        active.append(("b", path))
        expect("second tenant cancellation", call("b", "DELETE", path), 200)
        active.remove(("b", path))
    finally:
        for who, path in active:
            status, _ = call(who, "DELETE", path)
            if status not in (200, 404, 410):
                raise AssertionError("qualification cancellation unconfirmed; node expiry remains bounded")


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--kubeconfig", required=True)
    parser.add_argument("--output", required=True)
    args = parser.parse_args()
    session = Session(args.kubeconfig)
    verify(session)
    session.save(args.output)
    print(f"Passed {len(session.checks)} live admission checks.")
