import unittest
import subprocess
import tempfile
import json
from pathlib import Path
from unittest.mock import patch

from common import ContractError, instant, load, utc_text
from kind_runtime import KindRuntime, cgroup_path, command, metrics, projected_path
from sample import Window
from test_contract import ROOT
from workload import manifest


class ObserverTest(unittest.TestCase):
    def test_node_api_negotiates_schema_three_instead_of_reading_legacy_fields(self):
        with tempfile.TemporaryDirectory() as folder:
            r = object.__new__(KindRuntime)
            r.private = Path(folder)
            (r.private / "api-server").write_text("https://127.0.0.1:6443")
            legacy = json.dumps({"store": {"reliability": {"freshNodes": 1}}})
            r.k = lambda *args: legacy
            calls = []
            def run(argv):
                calls.append(argv)
                if "X-KubeMemLens-Snapshot-Schema: 3" not in argv:
                    return legacy
                return json.dumps({"store": {"nodeContext": {"freshRecords": 1}}})
            r.run = run
            result = r.api("/clusterstatus/current")
            self.assertEqual(result["store"]["nodeContext"]["freshRecords"], 1)
            self.assertIn("--cacert", calls[0])
            self.assertIn("--cert", calls[0])
            self.assertIn("--key", calls[0])
            self.assertNotIn("--insecure", calls[0])

    def test_projected_identity_uses_the_exact_runtime_mount_without_proc_access(self):
        source = "/var/lib/kubelet/pods/private/volumes/kubernetes.io~projected/kubelet-token"
        mount = {"destination": "/var/run/kubelet-token", "source": source}
        document = {"info": {"runtimeSpec": {"mounts": [mount]}}}
        self.assertEqual(projected_path(document), source + "/token")
        for value in ("/etc/private", source + "/../outside", source + "\nprivate"):
            mount["source"] = value
            with self.assertRaises(ContractError):
                projected_path(document)
        document["info"]["runtimeSpec"]["mounts"] = []
        with self.assertRaises(ContractError):
            projected_path(document)

    def test_command_failure_diagnostics_do_not_retain_output_or_arguments(self):
        error = subprocess.CalledProcessError(1, ["private-command"],
                                             stderr=b"private-node private-credential: Permission denied")
        with patch("kind_runtime.subprocess.run", side_effect=error):
            with self.assertRaises(ContractError) as caught:
                command(["private-command", "private-credential"])
        self.assertEqual(str(caught.exception), "local command failed: permission-denied")

    def test_live_timestamp_is_serialisable_and_matches_the_evidence_contract(self):
        import json
        timestamp = utc_text()
        self.assertEqual(json.loads(json.dumps(timestamp)), timestamp)
        self.assertEqual(instant(timestamp).isoformat(), timestamp.replace("Z", "+00:00"))

    def test_metrics_do_not_accept_missing_duplicate_or_nonfinite_numbers(self):
        self.assertEqual(metrics('# HELP a\na 0\nb{result="success"} 2\n'),
                         {"a": 0, 'b{result="success"}': 2})
        for text in ("a NaN", "a inf", "a -1", "a 0\na 1", "a 1 2"):
            with self.subTest(text=text), self.assertRaises(ContractError):
                metrics(text)

    def test_cgroup_location_stays_inside_observed_node(self):
        self.assertEqual(cgroup_path("/system.slice/kubelet.service"),
                         "/sys/fs/cgroup/system.slice/kubelet.service")
        for value in ("../private", "/../private", "/a\nb"):
            with self.assertRaises(ContractError):
                cgroup_path(value)

    def test_resource_deltas_measure_charged_memory_and_reject_reset(self):
        r = object.__new__(KindRuntime)
        r.previous = {}
        usage = [1000]
        def host(*args):
            return "0::/fixture\n" if args[-1].endswith("/cgroup") else (
                f"usage_usec {usage[0]}\n" if args[-1].endswith("cpu.stat") else "123456\n")
        r.host = host
        self.assertEqual(r.resources("kubelet", 10, 1), (None, 123456))
        usage[0] = 16000
        self.assertEqual(r.resources("kubelet", 10, 16), (1, 123456))
        usage[0] = 2
        with self.assertRaises(ContractError):
            r.resources("kubelet", 10, 31)

    def test_workload_uses_exact_bounded_profile_and_no_credentials(self):
        p = load(ROOT / "profiles/kind-137.json")
        spec = manifest(p, "fixture")["items"][1]["spec"]
        pod = spec["template"]["spec"]
        self.assertEqual(spec["replicas"] * len(pod["containers"]), 32)
        self.assertFalse(pod["automountServiceAccountToken"])
        self.assertTrue(all(c["image"] == p["workload"]["image"] for c in pod["containers"]))

    def test_rotation_needs_same_live_producer_and_later_success(self):
        class Runtime:
            identity = "first"
            reads = 1
            def resources(self, *_):
                return 1, 100
            def component_metrics(self, *_):
                return {'kubememlens_node_context_reads_total{result="success"}': self.reads,
                        'kubememlens_node_context_reads_total{result="access-denied"}': 0,
                        "kubememlens_node_context_last_read_seconds": .1,
                        "kubememlens_node_context_last_response_bytes": 1024}
            def projected_identity(self, *_):
                return self.identity
        r = Runtime()
        window = Window(r, "enabled")
        sample = {"elapsedSeconds": 15}
        producer = {"id": "same", "pid": 1}
        window.producer(producer, sample, 15)
        r.identity = "second"
        sample["elapsedSeconds"] = 480
        window.producer(producer, sample, 480)
        self.assertTrue(window.rotation["observed"])
        self.assertFalse(window.rotation["continuedAcquisition"])
        r.reads = 2
        window.producer(producer, sample, 495)
        self.assertTrue(window.rotation["continuedAcquisition"])
        with self.assertRaises(ContractError):
            window.producer({"id": "replacement", "pid": 2}, sample, 510)

    def test_owned_fixture_is_required_before_any_host_observation(self):
        calls = []
        def run(argv):
            calls.append(argv)
            return "unrelated-cluster\n"
        with self.assertRaises(ContractError):
            KindRuntime("kube-memlens-node-context-qualification", "fixture", "/private", run)
        self.assertEqual(len(calls), 1)
        self.assertEqual(calls[0][1], "inspect")


if __name__ == "__main__":
    unittest.main()
