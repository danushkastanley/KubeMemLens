import json
import tempfile
import unittest
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import patch

from common import ContractError
from verify_provider_artifacts import host_platform, run


class ArtifactCommandTest(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)
        (self.root / "plan.private.json").write_text(json.dumps({"planDigest": "sha256:" + "a" * 64}))
        (self.root / "profile.json").write_text('{}')
        self.args = SimpleNamespace(output=str(self.root / "result.json"), profile=str(self.root / "profile.json"),
                                    proposal=str(self.root), candidate_bundle="private-candidate", candidate_tag="v1.2.3-rc.1",
                                    image_archive="private-image", architecture="amd64")
        self.bundle = SimpleNamespace(configuration={"inventoryProfile": "gke-cos-containerd-amd64"})

    def test_success_is_evidence_only_and_never_run_approval(self):
        with patch("verify_provider_artifacts.validate_bundle", return_value=self.bundle), \
                patch("verify_provider_artifacts.verify", return_value={"sourceTreeDigest": "sha256:" + "b" * 64}), \
                patch("verify_provider_artifacts.host_platform", return_value="darwin_arm64"):
            run(self.args)
        result = json.loads(Path(self.args.output).read_text())
        self.assertFalse(result["qualified"])
        self.assertFalse(result["providerRunStarted"])
        self.assertEqual(result["scope"], "candidate-artefact-verification")
        self.assertEqual(Path(self.args.output).stat().st_mode & 0o777, 0o600)

    def test_profile_architecture_mismatch_stops_before_verification(self):
        self.args.architecture = "arm64"
        with patch("verify_provider_artifacts.validate_bundle", return_value=self.bundle), \
                patch("verify_provider_artifacts.verify") as verifier, self.assertRaises(ContractError):
            run(self.args)
        verifier.assert_not_called()
        self.assertFalse(Path(self.args.output).exists())

    def test_existing_output_is_preserved(self):
        Path(self.args.output).write_text("existing")
        with self.assertRaises(ContractError):
            run(self.args)
        self.assertEqual(Path(self.args.output).read_text(), "existing")

    def test_host_archive_selection_is_explicit_and_bounded(self):
        with patch("verify_provider_artifacts.platform.system", return_value="Darwin"), \
                patch("verify_provider_artifacts.platform.machine", return_value="arm64"):
            self.assertEqual(host_platform(), "darwin_arm64")
        with patch("verify_provider_artifacts.platform.system", return_value="Windows"), \
                patch("verify_provider_artifacts.platform.machine", return_value="x86_64"), self.assertRaises(ContractError):
            host_platform()


if __name__ == "__main__":
    unittest.main()
