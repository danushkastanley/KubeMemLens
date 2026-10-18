"""Private-input and runtime selection checks; no provider calls or benchmark claims."""
import json
from pathlib import Path
import tempfile
import unittest

from eks_host_runtime import EKSHostRuntime
from eks_idle_campaign import EKSIdleCampaign, private_configuration
from idle_campaign import Campaign
from local_runtime import Runtime


class EKSIdleCampaignTests(unittest.TestCase):
    def test_only_explicit_provider_entrypoint_changes_runtime(self):
        self.assertIs(Campaign.runtime_class, Runtime)
        self.assertIs(EKSIdleCampaign.runtime_class, EKSHostRuntime)
        self.assertIs(EKSIdleCampaign.run, Campaign.run)
        self.assertIs(EKSIdleCampaign.window, Campaign.window)

    def test_private_regular_input_and_duplicate_rejection(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'private.json'
            path.write_text(json.dumps({'providerExecution': {}})); path.chmod(0o600)
            self.assertEqual(private_configuration(path), {'providerExecution': {}})
            path.chmod(0o644)
            with self.assertRaisesRegex(ValueError, 'private owned'):
                private_configuration(path)
            path.chmod(0o600)
            for raw in ('{"providerExecution":{},"providerExecution":{}}',
                        '{"providerExecution":{"key":1,"key":2}}',
                        '{"providerExecution":NaN}', '{}', '[]', ' ' * 32769):
                path.write_text(raw)
                with self.subTest(raw=raw[:60]), self.assertRaises(ValueError):
                    private_configuration(path)
            link = Path(directory) / 'link'
            link.symlink_to(path)
            with self.assertRaises(OSError):
                private_configuration(link)


if __name__ == '__main__':
    unittest.main()
