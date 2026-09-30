import os
from pathlib import Path
import tempfile
import unittest

from bundle import snapshot
from manifest import Manifest, ManifestError, canonical
from test_manifest import fixture


class BundleTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.source = self.root/'input'
        self.source.mkdir()
        self.destination = self.root/'snapshot'
        self.manifest = Manifest.read(canonical(fixture()))
        for blob in self.manifest.blobs():
            (self.source/blob.name).write_bytes(b'bounded fixture')
        (self.source/'trace-release.json').write_bytes(self.manifest.original)
        (self.source/'release-subjects.txt').write_bytes(self.manifest.subjects())
        # Deliberately not a valid signature: snapshot must never claim trust.
        (self.source/'trace-release.sigstore.json').write_bytes(b'{}')

    def reject(self):
        with self.assertRaises(ManifestError):
            snapshot(self.source, self.destination)

    def test_snapshot_is_complete_private_and_independent_of_source(self):
        result = snapshot(self.source, self.destination)
        self.assertEqual(result, self.manifest)
        self.assertEqual(set(p.name for p in self.destination.iterdir()),
                         set(result.asset_names()))
        self.assertEqual(self.destination.stat().st_mode & 0o777, 0o700)
        for name in result.asset_names():
            copied = self.destination/name
            self.assertEqual(copied.read_bytes(), (self.source/name).read_bytes())
            self.assertEqual(copied.stat().st_mode & 0o777, 0o600)
        (self.source/'chart.tar').write_bytes(b'replaced')
        self.assertEqual((self.destination/'chart.tar').read_bytes(), b'bounded fixture')

    def test_existing_destination_is_never_replaced(self):
        self.destination.mkdir()
        (self.destination/'preserve').write_bytes(b'user-owned')
        self.reject()
        self.assertEqual((self.destination/'preserve').read_bytes(), b'user-owned')
        self.assertEqual(len(list(self.destination.iterdir())), 1)

    def test_extra_or_missing_subject_is_rejected_before_copying(self):
        (self.source/'unexpected').write_bytes(b'not in manifest')
        self.reject()
        self.assertFalse(self.destination.exists())
        (self.source/'unexpected').unlink()
        (self.source/'chart.tar').unlink()
        self.reject()
        self.assertFalse(self.destination.exists())

    def test_subject_list_cannot_substitute_its_own_inventory(self):
        (self.source/'release-subjects.txt').write_bytes(b'0'*64+b'  another.tar\n')
        self.reject()
        self.assertFalse(self.destination.exists())

    def test_tampered_payload_leaves_no_complete_snapshot_manifest(self):
        (self.source/'chart.tar').write_bytes(b'changed fixture')
        self.reject()
        self.assertFalse((self.destination/'trace-release.json').exists())

    def test_source_symlinks_are_rejected_without_following(self):
        victim = self.root/'outside'
        victim.write_bytes(b'bounded fixture')
        path = self.source/'chart.tar'
        path.unlink(); path.symlink_to(victim)
        self.reject()
        self.assertFalse(self.destination.exists())
        self.assertEqual(victim.read_bytes(), b'bounded fixture')

    def test_fifo_and_oversized_metadata_are_rejected(self):
        path = self.source/'chart.tar'
        path.unlink(); os.mkfifo(path)
        self.reject()
        path.unlink(); path.write_bytes(b'bounded fixture')
        (self.source/'trace-release.sigstore.json').write_bytes(b'x'*(4*1024*1024+1))
        self.reject()
        self.assertFalse(self.destination.exists())

    def test_directory_alias_and_source_are_not_mutated(self):
        alias = self.root/'alias'; alias.symlink_to(self.source, target_is_directory=True)
        with self.assertRaises(ManifestError):
            snapshot(alias, self.destination)
        self.assertEqual(set(p.name for p in self.source.iterdir()),
                         set(self.manifest.asset_names()))


if __name__ == '__main__':
    unittest.main()
