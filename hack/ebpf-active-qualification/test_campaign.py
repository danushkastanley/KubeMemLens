from pathlib import Path
import tempfile
import unittest
from unittest.mock import Mock, patch

from campaign import Campaign


class CampaignCleanupTests(unittest.TestCase):
    def test_api_restoration_precedes_namespace_cleanup_after_prepare_failure(self):
        with tempfile.TemporaryDirectory() as directory:
            run = Campaign.__new__(Campaign)
            run.directory = Path(directory)
            run.profile = {'pairs': 1}
            run.case = Mock()
            run.pairs = []
            run.fixtures = None
            run.record = Mock()
            run.progress = Mock()
            run.invariant = Mock()
            run.confirmed_profile = Mock()
            events = []
            run.restore = Mock(side_effect=lambda: events.append('restore-api'))
            fixtures = Mock()
            fixtures.prepare.side_effect = RuntimeError('setup failed')
            fixtures.cleanup.side_effect = lambda: events.append('cleanup-namespaces')
            with patch('campaign.Fixtures', return_value=fixtures):
                with self.assertRaisesRegex(RuntimeError, 'setup failed'):
                    run.run()
            self.assertEqual(events, ['restore-api', 'cleanup-namespaces'])


if __name__ == '__main__':
    unittest.main()
