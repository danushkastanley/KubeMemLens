#!/usr/bin/env python3

import copy
import importlib.util
import json
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parent
SPEC = importlib.util.spec_from_file_location("scale_evaluate", ROOT / "evaluate.py")
EVALUATOR = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(EVALUATOR)


def read_json(path):
    with path.open(encoding="utf-8") as source:
        return json.load(source)


class ScaleEvaluatorTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.development = read_json(ROOT / "development-smoke.json")
        cls.qualification = read_json(ROOT / "rc-5000.json")
        EVALUATOR.validate_profile(cls.development)
        EVALUATOR.validate_profile(cls.qualification)

    def fixture(self, name, profile):
        result = read_json(ROOT / "fixtures" / name)
        result["orchestrationOutcome"] = "completed"
        result["samplingFailureProbe"] = "none"
        result["disruptionOperational"] = {"unexplainedRestarts": 0, "oomKills": 0}
        result["profile"] = {"id": profile["id"], "digest": profile["profileDigest"]}
        result["workload"] = copy.deepcopy(profile["workload"])
        sample = result["samples"][0]
        minimum = profile["evidence"]["minimumSamples"]
        result["samples"] = [copy.deepcopy(sample) for _ in range(minimum)]
        interval = profile["workload"]["sampleIntervalSeconds"]
        for index, item in enumerate(result["samples"]):
            item["elapsedSeconds"] = index * interval
        measurements = result.get("measurements")
        if isinstance(measurements, dict):
            for key in ("agentScanMilliseconds", "cliLatencyMilliseconds", "tuiLatencyMilliseconds",
                        "nodeMemoryPressureNodes"):
                measurements[key] = measurements[key][:1] * minimum
            for component in measurements["components"].values():
                ratio = component["memoryLimitRatios"][0]
                component["memoryLimitRatios"] = [ratio] * minimum
                component["cpuThrottlingRatios"] = component["cpuThrottlingRatios"][:1] * minimum
            measurements["agentPostFailures"] = [0] * minimum
            measurements["agentScanFailures"] = [0] * minimum
            measurements["canary"]["higherIsBetter"] = False
            measurements["canary"]["control"] = measurements["canary"]["control"][:1] * profile["evidence"]["canaryControlSamples"]
            observed = 105 if name == "qualification-boundary-pass.json" else measurements["canary"]["observed"][0]
            measurements["canary"]["observed"] = [observed] * minimum
        return result

    def checks_by_name(self, report):
        return {item["name"]: item for item in report["checks"]}

    def test_profile_digests_are_self_authenticating(self):
        for profile in (self.development, self.qualification):
            self.assertEqual(profile["profileDigest"], EVALUATOR.canonical_digest(profile))
        changed = copy.deepcopy(self.qualification)
        changed["workload"]["containers"] += 1
        with self.assertRaisesRegex(EVALUATOR.EvaluationInputError, "profileDigest"):
            EVALUATOR.validate_profile(changed)

    def test_checked_in_fixtures_match_the_summary_contract(self):
        for path in sorted((ROOT / "fixtures").glob("*.json")):
            summary = read_json(path)
            if path.name == "qualification-forbidden-identifier.json":
                with self.assertRaisesRegex(EVALUATOR.EvaluationInputError, "forbidden key"):
                    EVALUATOR.validate_summary(summary)
            else:
                EVALUATOR.validate_summary(summary)

    def test_qualification_cannot_disable_required_telemetry(self):
        changed = copy.deepcopy(self.qualification)
        changed["telemetryRequired"] = False
        changed["profileDigest"] = EVALUATOR.canonical_digest(changed)
        with self.assertRaisesRegex(EVALUATOR.EvaluationInputError, "require telemetry"):
            EVALUATOR.validate_profile(changed)

    def test_profile_duration_must_align_with_sampling_interval(self):
        changed = copy.deepcopy(self.qualification)
        changed["workload"]["steadyStateSeconds"] = 1801
        changed["profileDigest"] = EVALUATOR.canonical_digest(changed)
        with self.assertRaisesRegex(EVALUATOR.EvaluationInputError, "divide exactly"):
            EVALUATOR.validate_profile(changed)

    def test_profile_runtime_bounds_are_canonical(self):
        for field, value, message in (
            ("containersPerPod", 100, "between 1 and 50"),
            ("sampleIntervalSeconds", 1, "between 5 and 300"),
            ("creationBatchPods", 101, "cannot exceed"),
        ):
            with self.subTest(field=field):
                changed = copy.deepcopy(self.qualification)
                changed["workload"][field] = value
                changed["profileDigest"] = EVALUATOR.canonical_digest(changed)
                with self.assertRaisesRegex(EVALUATOR.EvaluationInputError, message):
                    EVALUATOR.validate_profile(changed)

    def test_nearest_rank_percentiles(self):
        values = list(range(1, 21))
        self.assertEqual(EVALUATOR.nearest_rank(values, 95), 19)
        self.assertEqual(EVALUATOR.nearest_rank(values, 99), 20)
        self.assertEqual(EVALUATOR.nearest_rank([3, 1, 2], 50), 2)

    def test_qualification_inclusive_boundaries_pass(self):
        summary = self.fixture("qualification-boundary-pass.json", self.qualification)
        report = EVALUATOR.evaluate(self.qualification, summary)
        self.assertEqual(report["result"], "pass", report["failures"])
        self.assertTrue(all(item["status"] == "pass" for item in report["checks"]))
        checks = self.checks_by_name(report)
        self.assertEqual(checks["recovery"]["observed"]["partialRollout"], 120)
        self.assertEqual(checks["agent_post_failures"]["observed"], 0)
        self.assertEqual(checks["node_memory_pressure"]["observed"], [0] * self.qualification["evidence"]["minimumSamples"])
        self.assertEqual(checks["canary_regression"]["observed"]["regressionPercent"], 5)

    def test_exclusive_boundaries_fail(self):
        summary = self.fixture("qualification-exclusive-boundary-fail.json", self.qualification)
        report = EVALUATOR.evaluate(self.qualification, summary)
        checks = self.checks_by_name(report)
        self.assertEqual(report["result"], "fail")
        self.assertEqual(checks["agent_scan_p99"]["status"], "fail")
        self.assertEqual(checks["cpu_throttling_p95"]["status"], "fail")
        self.assertEqual(checks["component_memory_p95"]["status"], "pass")
        self.assertEqual(checks["recovery"]["status"], "pass")

    def test_qualification_missing_telemetry_fails_closed(self):
        summary = self.fixture("qualification-missing-telemetry.json", self.qualification)
        report = EVALUATOR.evaluate(self.qualification, summary)
        checks = self.checks_by_name(report)
        required = {
            "agent_scan_p99", "cli_latency_p95", "tui_latency_p95", "recovery", "component_memory_p95",
            "cpu_throttling_p95", "agent_post_failures", "agent_scan_failures", "api_server",
            "node_memory_pressure", "canary_regression",
        }
        self.assertEqual(report["result"], "fail")
        self.assertTrue(required.issubset({name for name, item in checks.items() if item["status"] == "fail"}))
        self.assertEqual(len(report["failures"]), len(required))

    def test_qualification_node_memory_pressure_fails(self):
        summary = self.fixture("qualification-boundary-pass.json", self.qualification)
        summary["measurements"]["nodeMemoryPressureNodes"][1] = 1
        report = EVALUATOR.evaluate(self.qualification, summary)
        self.assertEqual(report["result"], "fail")
        self.assertEqual(self.checks_by_name(report)["node_memory_pressure"]["status"], "fail")

    def test_qualification_requires_the_declared_steady_state_window(self):
        summary = self.fixture("qualification-boundary-pass.json", self.qualification)
        for index, sample in enumerate(summary["samples"]):
            sample["elapsedSeconds"] = index
        report = EVALUATOR.evaluate(self.qualification, summary)
        self.assertEqual(report["result"], "fail")
        self.assertEqual(self.checks_by_name(report)["steady_state_window"]["status"], "fail")

    def test_qualification_rejects_failed_orchestration(self):
        summary = self.fixture("qualification-boundary-pass.json", self.qualification)
        summary["orchestrationOutcome"] = "failed"
        summary["samplingFailureProbe"] = "mapping"
        report = EVALUATOR.evaluate(self.qualification, summary)
        self.assertEqual(report["result"], "fail")
        self.assertEqual(self.checks_by_name(report)["orchestration"]["status"], "fail")

    def test_sampling_failure_probe_is_strict_and_absent_on_success(self):
        summary = self.fixture("qualification-boundary-pass.json", self.qualification)
        summary["samplingFailureProbe"] = "mapping"
        with self.assertRaisesRegex(EVALUATOR.EvaluationInputError, "successful orchestration"):
            EVALUATOR.validate_summary(summary)

        summary["orchestrationOutcome"] = "failed"
        EVALUATOR.validate_summary(summary)
        summary["samplingFailureProbe"] = "pod-name"
        with self.assertRaisesRegex(EVALUATOR.EvaluationInputError, "sampling failure probe"):
            EVALUATOR.validate_summary(summary)

    def test_qualification_rejects_disruption_anomalies(self):
        summary = self.fixture("qualification-boundary-pass.json", self.qualification)
        summary["disruptionOperational"]["oomKills"] = 1
        report = EVALUATOR.evaluate(self.qualification, summary)
        self.assertEqual(report["result"], "fail")
        self.assertEqual(self.checks_by_name(report)["disruption_stability"]["status"], "fail")

    def test_qualification_requires_exact_full_density_replacement(self):
        for field, value in (
            ("observedPods", 9),
            ("observedPods", 11),
            ("residentContainersBefore", 4950),
            ("residentContainersAfter", 4950),
        ):
            with self.subTest(field=field, value=value):
                summary = self.fixture("qualification-boundary-pass.json", self.qualification)
                summary["workloadReplacement"][field] = value
                report = EVALUATOR.evaluate(self.qualification, summary)
                self.assertEqual(report["result"], "fail")
                self.assertEqual(self.checks_by_name(report)["workload_replacement"]["status"], "fail")

    def test_qualification_rejects_sparse_telemetry(self):
        summary = self.fixture("qualification-boundary-pass.json", self.qualification)
        summary["measurements"]["agentScanMilliseconds"] = [1]
        summary["measurements"]["components"]["agent"]["memoryLimitRatios"] = [0.1]
        report = EVALUATOR.evaluate(self.qualification, summary)
        self.assertEqual(report["result"], "fail")
        self.assertEqual(self.checks_by_name(report)["agent_scan_p99"]["status"], "fail")
        self.assertEqual(self.checks_by_name(report)["component_memory_p95"]["status"], "fail")

    def test_canary_direction_is_fixed_to_latency(self):
        summary = self.fixture("qualification-boundary-pass.json", self.qualification)
        summary["measurements"]["canary"]["higherIsBetter"] = True
        report = EVALUATOR.evaluate(self.qualification, summary)
        self.assertEqual(report["result"], "fail")
        self.assertEqual(self.checks_by_name(report)["canary_regression"]["status"], "fail")

    def test_safe_summary_privacy_shape_is_accepted(self):
        summary = self.fixture("qualification-boundary-pass.json", self.qualification)
        EVALUATOR.validate_summary(summary)
        self.assertEqual(EVALUATOR.evaluate(self.qualification, summary)["result"], "pass")

    def test_privacy_and_caveat_shape_is_strict(self):
        summary = self.fixture("qualification-boundary-pass.json", self.qualification)
        summary["privacy"]["rawLogsIncluded"] = 0
        with self.assertRaisesRegex(EVALUATOR.EvaluationInputError, "privacy"):
            EVALUATOR.validate_summary(summary)
        summary = self.fixture("qualification-boundary-pass.json", self.qualification)
        summary["caveats"] = ["x" * (EVALUATOR.MAX_CAVEAT_LENGTH + 1)]
        with self.assertRaisesRegex(EVALUATOR.EvaluationInputError, "caveats"):
            EVALUATOR.validate_summary(summary)

    def test_identifier_bearing_summary_is_input_invalid(self):
        summary = self.fixture("qualification-forbidden-identifier.json", self.qualification)
        with self.assertRaisesRegex(EVALUATOR.EvaluationInputError, "podName"):
            EVALUATOR.evaluate(self.qualification, summary)
        safe = self.fixture("qualification-boundary-pass.json", self.qualification)
        safe["samples"][0]["nodeName"] = "kind-worker"
        with self.assertRaisesRegex(EVALUATOR.EvaluationInputError, "nodeName"):
            EVALUATOR.evaluate(self.qualification, safe)

        with tempfile.TemporaryDirectory() as directory:
            summary_path = Path(directory) / "summary.json"
            summary_path.write_text(json.dumps(summary), encoding="utf-8")
            result = subprocess.run([
                sys.executable, str(ROOT / "evaluate.py"), "--profile", str(ROOT / "rc-5000.json"),
                "--summary", str(summary_path),
            ], check=False, capture_output=True, text=True)
        self.assertEqual(result.returncode, 2)
        self.assertIn("forbidden key: podName", result.stderr)

    def test_development_missing_telemetry_is_explicit(self):
        summary = self.fixture("development-missing-telemetry.json", self.development)
        report = EVALUATOR.evaluate(self.development, summary)
        advanced_names = {
            "agent_scan_p99", "cli_latency_p95", "tui_latency_p95", "recovery", "component_memory_p95",
            "cpu_throttling_p95", "agent_post_failures", "agent_scan_failures", "api_server",
            "node_memory_pressure", "canary_regression",
        }
        advanced = [item for item in report["checks"] if item["name"] in advanced_names]
        self.assertEqual(report["result"], "pass", report["failures"])
        self.assertTrue(advanced)
        self.assertTrue(all(item["status"] == "not_evaluated" for item in advanced))

    def test_identity_mismatch_fails(self):
        summary = self.fixture("qualification-boundary-pass.json", self.qualification)
        summary["profile"]["digest"] = "sha256:" + "0" * 64
        report = EVALUATOR.evaluate(self.qualification, summary)
        self.assertEqual(self.checks_by_name(report)["profile_identity"]["status"], "fail")

    def test_non_object_summary_is_rejected(self):
        with self.assertRaisesRegex(EVALUATOR.EvaluationInputError, "JSON object"):
            EVALUATOR.evaluate(self.qualification, [])

    def test_cli_output_is_deterministic(self):
        summary = self.fixture("qualification-boundary-pass.json", self.qualification)
        with tempfile.TemporaryDirectory() as directory:
            summary_path = Path(directory) / "summary.json"
            summary_path.write_text(json.dumps(summary), encoding="utf-8")
            command = [
                sys.executable, str(ROOT / "evaluate.py"), "--profile", str(ROOT / "rc-5000.json"),
                "--summary", str(summary_path),
            ]
            first = subprocess.run(command, check=False, capture_output=True, text=True)
            second = subprocess.run(command, check=False, capture_output=True, text=True)
        self.assertEqual(first.returncode, 0, first.stderr)
        self.assertEqual(first.stdout, second.stdout)
        self.assertEqual(json.loads(first.stdout)["result"], "pass")


if __name__ == "__main__":
    unittest.main()
