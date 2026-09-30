from pathlib import Path
import tempfile
import unittest

from profile import load_profile
from provenance import stream_names, write_envelope, verify_envelope


class ProvenanceTests(unittest.TestCase):
    def test_modified_missing_and_cross_profile_streams_fail(self):
        profile = load_profile()
        cfg = {'sourceSHA256': 'a' * 64}
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            for name in stream_names('control', 36) + ['window.private.json']:
                (root / name).write_text('{}\n')
            write_envelope(root, 'control', 1, profile, cfg, 'kernel-1')
            self.assertEqual(verify_envelope(root, 'control', profile)['bootID'], 'kernel-1')
            with self.assertRaises(ValueError):
                verify_envelope(root, 'enabled', profile)
            with self.assertRaises(ValueError):
                verify_envelope(root, 'control', {**profile, 'windowSeconds': 30})
            (root / 'workload.jsonl').write_text('{"changed":true}\n')
            with self.assertRaises(ValueError):
                verify_envelope(root, 'control', profile)
            (root / 'workload.jsonl').unlink()
            with self.assertRaises(FileNotFoundError):
                verify_envelope(root, 'control', profile)


if __name__ == '__main__':
    unittest.main()
