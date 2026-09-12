import json
import unittest
from unittest.mock import patch

from common import ContractError, digest, load
from prepare_provider import REPOSITORY, local_command, prepare
from provider_bundle import validate_bundle
from provider_plan import file_digest
from test_provider_plan import ProviderFixture


class BundleTest(ProviderFixture, unittest.TestCase):
    def setUp(self):
        self.profile = load(REPOSITORY / "hack/node-qualification/profiles/gke-standard.json")
        self.output = self.root / self._testMethodName
        # Model the clean-checkout boundary while still exercising real Git blob
        # binding, chart validation and Helm rendering in this unit fixture.
        with patch("prepare_provider.local_command", side_effect=self.clean_git):
            self.plan = prepare(self.profile, self.config(), self.output)

    def clean_git(self, args, repository):
        if args[:2] == ["git", "status"]:
            return b""
        return local_command(args, repository)

    def reseal(self):
        self.plan["planDigest"] = digest(self.plan, "planDigest")
        (self.output / "plan.private.json").write_text(json.dumps(self.plan))

    def validate(self, command=None, acknowledged=None):
        return validate_bundle(self.output, self.profile, acknowledged or self.plan["planDigest"],
                               command=command or self.clean_git)

    def test_valid_bundle_never_executes_kubernetes_credentials(self):
        result = self.validate()
        self.assertEqual(result.configuration["namespace"], self.config()["namespace"])
        self.assertFalse(result.plan["providerRunStarted"])
        self.assertFalse((self.root / "credential-executed").exists())

    def test_acknowledgement_binds_exact_plan_digest(self):
        with self.assertRaises(ContractError):
            self.validate(acknowledged="sha256:" + "f" * 64)

    def test_dirty_source_cannot_pass_as_immutable(self):
        def dirty(args, repository):
            return b" M source\n" if args[:2] == ["git", "status"] else self.clean_git(args, repository)
        with self.assertRaisesRegex(ContractError, "clean checkout"):
            self.validate(command=dirty)
        self.plan["toolSourceDirty"] = True; self.reseal()
        with self.assertRaises(ContractError):
            self.validate()

    def test_rehashed_privilege_change_is_rejected_by_semantic_validation(self):
        path = self.output / "host-observers.json"
        host = load(path)
        host["items"][1]["spec"]["template"]["spec"]["containers"][0]["securityContext"]["privileged"] = True
        path.write_text(json.dumps(host))
        self.plan["files"][path.name] = file_digest(path, 2 * 1024 * 1024); self.reseal()
        with self.assertRaisesRegex(ContractError, "fixed generator"):
            self.validate()

    def test_changed_bytes_or_extra_files_are_not_adopted(self):
        path = self.output / "baseline-values.json"
        path.write_text(path.read_text() + "\n")
        with self.assertRaisesRegex(ContractError, "bytes changed"):
            self.validate()
        (self.output / "unexpected").write_text("extra")
        with self.assertRaisesRegex(ContractError, "file set changed"):
            self.validate()

    def test_shared_files_and_symlinks_are_rejected(self):
        path = self.output / "enabled-values.json"
        path.chmod(0o644)
        with self.assertRaisesRegex(ContractError, "private regular"):
            self.validate()
        path.unlink(); path.symlink_to(self.output / "baseline-values.json")
        with self.assertRaisesRegex(ContractError, "private regular"):
            self.validate()


if __name__ == "__main__":
    unittest.main()
