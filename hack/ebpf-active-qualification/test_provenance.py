from pathlib import Path
import tempfile
import unittest

from profile import load_profile
from campaign import PROFILES
from provenance import profile_stream_names, write_envelope, verify_envelope


class ProvenanceTests(unittest.TestCase):
    def test_modified_missing_and_cross_profile_streams_fail(self):
        profile = load_profile()
        cfg = {'sourceSHA256': 'a' * 64}
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            for name in profile_stream_names('control', profile) + ['window.private.json']:
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

    def test_every_profile_requires_unchanged_verifier_stream_input_and_journal(self):
        for name, load in PROFILES.items():
            for phase in ('control', 'enabled'):
                with self.subTest(profile=name, phase=phase), tempfile.TemporaryDirectory() as directory:
                    root = Path(directory)
                    profile = load()
                    names = profile_stream_names(phase, profile)
                    required = {'verifier.jsonl', 'verifier-input.private.json', 'verifier-probes.private.json'}
                    self.assertTrue(required.issubset(names))
                    for item in names + ['window.private.json']:
                        (root / item).write_text('{}\n')
                    write_envelope(root, phase, 1, profile, {'sourceSHA256': 'a' * 64}, 'boot')
                    for item in required:
                        (root / item).write_text('{"changed":true}\n')
                        with self.assertRaises(ValueError): verify_envelope(root, phase, profile)
                        (root / item).unlink()
                        with self.assertRaises(FileNotFoundError): verify_envelope(root, phase, profile)
                        (root / item).write_text('{}\n')
                    verify_envelope(root, phase, profile)


if __name__ == '__main__':
    unittest.main()
