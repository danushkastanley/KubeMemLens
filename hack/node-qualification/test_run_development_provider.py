"""Exercise the shared protocol with development evidence and fake target adapters."""

import unittest
from unittest.mock import patch

from common import ContractError, load
from evaluate import evaluate
from run_development_provider import ACKNOWLEDGEMENT, run
import test_run_provider


class DevelopmentProtocolTest(unittest.TestCase):
    def setUp(self):
        self.case = test_run_provider.ProviderCommandTest()
        self.case.setUp()
        self.addCleanup(self.case.doCleanups)
        self.case.args.acknowledge_development = ACKNOWLEDGEMENT
        self.case.fixture.proof.update(authority="local-development", releaseQualificationGranted=False)

    def run_command(self):
        c = self.case
        with patch("run_development_provider.prepare_with", return_value=(
                c.e.bundle, c.fixture.proof, c.private, c.public)), patch("test_run_provider.run", run):
            return c.run_command()

    def observations(self):
        result = {}
        for path in self.case.public.glob("*.json"):
            value = load(path)
            self.assertEqual(set(value), {"schemaVersion", "scope", "authority", "qualified",
                                          "releaseQualificationGranted", "observation"})
            self.assertEqual(value["scope"], "development-provider-experiment")
            self.assertEqual(value["authority"], "local-development")
            self.assertIs(value["qualified"], False)
            self.assertIs(value["releaseQualificationGranted"], False)
            self.assertNotIn("private-", path.read_text())
            self.assertEqual(path.stat().st_mode & 0o777, 0o600)
            result[path.name] = value["observation"]
            with self.assertRaises(ContractError):
                evaluate(self.case.fixture.profile, value)
        self.assertTrue(result)
        return result

    def test_complete_protocol_preserves_measurement_recovery_replacement_and_cleanup(self):
        result = self.run_command()
        self.assertIs(result["releaseQualificationGranted"], False)
        self.assertEqual(result["protocolState"], "awaiting-independent-provider-cleanup")
        self.assertEqual(self.case.events, ["prepare", "baseline", "enable", "enabled", "replacement", "cleanup"])
        records = self.observations()
        expected = {"provider-inventory.json", "baseline-measurements.json", "enabled-measurements.json",
                    "sourceLoss.json", "agentRestart.json", "collectorRestart.json", "replacement.json",
                    "network-policy.json", "production-cli.json", "final-live-images.json",
                    "qualification-observations.json", "kubernetes-cleaned-observations.json",
                    "qualification-evaluation.pending.json"}
        self.assertEqual(set(records), expected)
        self.assertTrue(records["kubernetes-cleaned-observations.json"]["cleanup"]["workloadsRemoved"])
        self.assertEqual(records["kubernetes-cleaned-observations.json"]["cleanup"]["cloudResources"], "pending")
        self.assertEqual([c["id"] for c in records["qualification-evaluation.pending.json"]["checks"] if not c["passed"]], ["cleanup"])

    def test_failed_budget_cleans_up_and_does_not_replace_nodes(self):
        self.case.fixture.example["nodes"][1]["samples"]["enabled"][3]["producerCPUMilli"] = 100000
        with self.assertRaises(ContractError):
            self.run_command()
        self.case.replacer.run.assert_not_called()
        self.case.e.cleanup.assert_called_once_with()
        self.assertEqual(self.observations()["failure.json"]["stage"], "measurement-validation")

    def test_failed_preparation_retains_failure_and_cleans_owned_resources(self):
        self.case.e.prepare.side_effect = ContractError("private-credential")
        with self.assertRaises(ContractError):
            self.run_command()
        self.case.e.cleanup.assert_called_once_with()
        failure = self.observations()["failure.json"]
        self.assertEqual(failure["stage"], "preparation")
        self.assertEqual(failure["ownedCleanup"], "passed")

    def test_interruption_cleans_up_without_emitting_release_evidence(self):
        self.case.e.measure.side_effect = KeyboardInterrupt
        with self.assertRaises(KeyboardInterrupt):
            self.run_command()
        self.case.e.cleanup.assert_called_once_with()
        self.assertEqual(self.observations()["failure.json"]["failureType"], "KeyboardInterrupt")

    def test_cleanup_failure_cannot_emit_cleaned_record(self):
        self.case.e.cleanup.side_effect = ContractError("private-resource")
        with self.assertRaises(ContractError):
            self.run_command()
        records = self.observations()
        self.assertEqual(records["failure.json"]["ownedCleanup"], "failed")
        self.assertNotIn("kubernetes-cleaned-observations.json", records)

    def test_explicit_development_acknowledgement_precedes_all_preparation(self):
        self.case.args.acknowledge_development = "run-reviewed-node-context-plan"
        with patch("run_development_provider.prepare_with") as prepare, self.assertRaises(ContractError):
            run(self.case.args)
        prepare.assert_not_called()

    def test_release_authority_cannot_enter_development_protocol(self):
        self.case.fixture.proof["authority"] = "release"
        with self.assertRaises(ContractError):
            self.run_command()
        self.case.helpers.assert_not_called()
        self.case.e.prepare.assert_not_called()


if __name__ == "__main__":
    unittest.main()
