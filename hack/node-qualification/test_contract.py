import copy
import json
import pathlib
import tempfile
import unittest

from common import ContractError, digest, load, write_new
from evaluate import cpu_mean, evaluate
from evidence import ARTEFACTS, EVENTS, FIELDS, PRIVACY, validate_evidence
from profiles import validate_profile

ROOT = pathlib.Path(__file__).resolve().parent


def fixture(profile=None):
    p = copy.deepcopy(profile or load(ROOT / "profiles/kind-137.json"))
    e = {"schemaVersion": 1, "profile": {"id": p["id"], "digest": p["profileDigest"]}, "startedAt": "2026-09-11T12:00:00Z", "completedAt": "2026-09-11T12:20:00Z", "orchestration": "completed",
         "artefacts": {k: False if k == "sourceDirty" else "a" * 40 if k.endswith("Commit") else "sha256:" + "a" * 64 for k in ARTEFACTS},
         "environment": {"provider": p["provider"], "kubernetes": "v1.37.0", "kernel": "7.0.12-linuxkit", "runtime": "containerd://2.3.1", "nodeImage": p["nodeImage"] if p["provider"] == "kind" else "COS_CONTAINERD", "osImage": "Debian Linux", "architecture": "arm64", "cgroupVersion": "v2", "cni": "kindnet", "linuxNodes": p["workload"]["linuxNodes"], "providerReceiptDigest": None if p["provider"] == "kind" else "sha256:" + "b" * 64},
         "transport": {"result": "passed", "reason": "none", "directTLS": True, "podBoundIdentity": True, "statsOnlyRBAC": True, "proxyAccess": False, "networkPolicy": "not-qualified" if p["provider"] == "kind" else "passed", "servingTrust": "fixture-ca" if p["provider"] == "kind" else "provider-verified"},
         "fields": {k: "available" for k in FIELDS}, "provenance": "unknown", "samples": {"baseline": [], "enabled": []},
         "rotation": {"observed": True, "sameProducer": True, "continuedAcquisition": True, "elapsedSeconds": 480},
         "lifecycle": {k: {"state": "passed", "elapsedSeconds": 30, "identityVerified": True, "freshEvidence": True, "staleRetained": k == "sourceLoss"} for k in EVENTS},
         "cleanup": {"workloadsRemoved": True, "rbacRemoved": True, "cloudResources": "not-applicable" if p["provider"] == "kind" else "confirmed"}, "privacy": dict(PRIVACY)}
    if p["provider"] == "kind":
        e["lifecycle"]["providerNodeReplacement"] = {"state": "not-applicable", "elapsedSeconds": None, "identityVerified": False, "freshEvidence": False, "staleRetained": False}
    for phase, count in (("baseline", 8), ("enabled", 44)):
        for i in range(count):
            enabled = phase == "enabled"
            e["samples"][phase].append({"elapsedSeconds": (i + 1) * 15, "producerCPUMilli": 10 if enabled else None, "producerMemoryBytes": 16 * 1024 * 1024 if enabled else None,
                "kubeletCPUMilli": 30 if enabled else 20, "kubeletMemoryBytes": 64 * 1024 * 1024, "agentScanSeconds": .02,
                "sourceReads": i + 1 if enabled else None, "sourceFailures": 0 if enabled else None, "lastReadSeconds": .02 if enabled else None, "lastResponseBytes": 10000 if enabled else None, "workloadContainers": p["workload"]["containers"], "mappedContainers": p["workload"]["containers"], "freshNodes": p["workload"]["linuxNodes"], "producerReplicas": p["workload"]["linuxNodes"] if enabled else 0, "unexpectedRestarts": 0, "unexpectedOOMKills": 0})
    seal(e)
    return p, e


def seal(e):
    e["recordDigest"] = digest(e, "recordDigest")


class QualificationContractTest(unittest.TestCase):
    def test_all_profiles_are_canonical_and_do_not_claim_results(self):
        for path in (ROOT / "profiles").glob("*.json"):
            with self.subTest(path=path.name):
                p = validate_profile(load(path))
                self.assertNotIn("qualified", p)
                self.assertEqual(p["measurement"]["projectedLifetimeSeconds"], 600)

    def test_passing_measurements_do_not_self_qualify(self):
        p, e = fixture()
        result = evaluate(p, e)
        self.assertEqual(result["outcome"], "pass", result["checks"])
        self.assertFalse(result["qualified"])
        self.assertEqual(result["reviewState"], "pending")
        self.assertEqual(result["provenance"], "unknown")

    def test_missing_or_adverse_observations_fail_without_becoming_zero(self):
        changes = {
            "missing metric": lambda e: e["samples"]["enabled"][3].update(producerCPUMilli=None),
            "high cpu": lambda e: [s.update(producerCPUMilli=100) for s in e["samples"]["enabled"]],
            "high memory": lambda e: e["samples"]["enabled"][3].update(producerMemoryBytes=100000000),
            "counter reset": lambda e: e["samples"]["enabled"][20].update(sourceReads=0),
            "failures": lambda e: e["samples"]["enabled"][-1].update(sourceFailures=1),
            "duplicates": lambda e: [s.update(sourceReads=1) for s in e["samples"]["enabled"]],
            "short window": lambda e: e["samples"].update(enabled=e["samples"]["enabled"][:20]),
            "empty baseline": lambda e: e["samples"].update(baseline=[]),
            "no rotation": lambda e: e["rotation"].update(observed=False),
            "new producer": lambda e: e["rotation"].update(sameProducer=False),
            "no stale retention": lambda e: e["lifecycle"]["sourceLoss"].update(staleRetained=False),
            "slow recovery": lambda e: e["lifecycle"]["agentRestart"].update(elapsedSeconds=121),
            "proxy": lambda e: e["transport"].update(proxyAccess=True),
            "cleanup": lambda e: e["cleanup"].update(rbacRemoved=False),
            "missing load": lambda e: e["samples"]["enabled"][2].update(workloadContainers=0),
            "mapping": lambda e: e["samples"]["enabled"][2].update(mappedContainers=1),
            "restart": lambda e: e["samples"]["enabled"][2].update(unexpectedRestarts=1),
            "unsupported cgroup": lambda e: e["environment"].update(cgroupVersion="v1"),
            "unreported architecture": lambda e: e["environment"].update(architecture="unreported"),
        }
        for name, change in changes.items():
            with self.subTest(name=name):
                p, e = fixture(); change(e); seal(e)
                self.assertEqual(evaluate(p, e)["outcome"], "fail")

    def test_exclusive_scan_boundary_and_inclusive_read_budget(self):
        p, e = fixture()
        for s in e["samples"]["enabled"]:
            s["lastReadSeconds"] = 1
        seal(e)
        self.assertEqual(evaluate(p, e)["outcome"], "pass")
        e["samples"]["enabled"][-1]["agentScanSeconds"] = 4
        seal(e)
        self.assertEqual(evaluate(p, e)["outcome"], "fail")

    def test_profile_changes_require_a_new_digest(self):
        p, _ = fixture(); p["budgets"]["readP95Seconds"] = 2
        with self.assertRaises(ContractError):
            validate_profile(p)

    def test_unreported_environment_metadata_cannot_pass(self):
        for field in ("kernel", "runtime", "osImage", "cni"):
            for value in ("unreported", "unknown", " N/A "):
                with self.subTest(field=field, value=value):
                    p, e = fixture()
                    e["environment"][field] = value
                    seal(e)
                    self.assertEqual(evaluate(p, e)["outcome"], "fail")
        p, e = fixture(load(ROOT / "profiles/gke-standard.json"))
        e["environment"]["nodeImage"] = "provider-receipt"
        seal(e)
        self.assertEqual(evaluate(p, e)["outcome"], "fail")

    def test_identifiers_and_credentials_are_rejected(self):
        for key, value in (("podUID", "private"), ("token", "secret"), ("namespace", "private")):
            p, e = fixture(); e[key] = value; seal(e)
            with self.assertRaises(ContractError):
                validate_evidence(p, e)
        for value in ("Bearer abcdefghijklmnop", "10.20.30.40", "arn:aws:ec2:eu-west-1:123456789012:instance/i-abcd"):
            p, e = fixture(); e["environment"]["nodeImage"] = value; seal(e)
            with self.assertRaises(ContractError):
                validate_evidence(p, e)

    def test_provider_requires_independent_environment_and_cleanup_evidence(self):
        p = load(ROOT / "profiles/gke-standard.json"); p, e = fixture(p)
        self.assertEqual(evaluate(p, e)["outcome"], "pass")
        e["cleanup"]["cloudResources"] = "pending"; seal(e)
        self.assertEqual(evaluate(p, e)["outcome"], "fail")
        e["environment"]["providerReceiptDigest"] = None; seal(e)
        with self.assertRaises(ContractError):
            validate_evidence(p, e)

    def test_bounded_loader_rejects_duplicates_and_deep_inputs(self):
        with tempfile.TemporaryDirectory() as folder:
            path = pathlib.Path(folder) / "evidence.json"
            for text in ('{"schemaVersion":1,"schemaVersion":1}', '[' * 30 + '0' + ']' * 30, ' ' * (512 * 1024 + 1)):
                path.write_text(text)
                with self.assertRaises(ContractError):
                    load(path)

    def test_atomic_output_is_private_and_does_not_replace(self):
        with tempfile.TemporaryDirectory() as folder:
            path = pathlib.Path(folder) / "result.json"
            write_new(path, {"schemaVersion": 1})
            self.assertEqual(path.stat().st_mode & 0o777, 0o600)
            before = path.read_bytes()
            with self.assertRaises(FileExistsError):
                write_new(path, {"schemaVersion": 2})
            self.assertEqual(path.read_bytes(), before)
            self.assertEqual(len(list(path.parent.iterdir())), 1)

    def test_boolean_schema_and_numeric_privacy_flags_are_invalid(self):
        p, e = fixture(); e["schemaVersion"] = True; seal(e)
        with self.assertRaises(ContractError):
            validate_evidence(p, e)

        p, e = fixture(); e["privacy"]["identifiersIncluded"] = 0; seal(e)
        with self.assertRaises(ContractError):
            validate_evidence(p, e)

    def test_cpu_means_weight_measured_intervals(self):
        samples = [{"elapsedSeconds": 15, "cpu": 0}, {"elapsedSeconds": 45, "cpu": 90}]
        self.assertEqual(cpu_mean(samples, "cpu"), 60)
        samples[1]["cpu"] = None
        self.assertIsNone(cpu_mean(samples, "cpu"))


if __name__ == "__main__":
    unittest.main()
