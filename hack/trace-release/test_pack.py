import hashlib
import os
from pathlib import Path
import tarfile
import tempfile
import unittest

from manifest import ManifestError
from pack import pack_tree


class ArchiveProducerTests(unittest.TestCase):
    def test_archive_is_reproducible_without_host_ownership_or_timestamps(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            source = root/'source'; source.mkdir()
            member = source/'source.c'; member.write_bytes(b'public source\n'); member.chmod(0o444)
            one, two = root/'one.tar', root/'two.tar'
            pack_tree(source, one, 1000)
            os.utime(member, (2000, 2000))
            pack_tree(source, two, 1000)
            self.assertEqual(hashlib.sha256(one.read_bytes()).digest(), hashlib.sha256(two.read_bytes()).digest())
            with tarfile.open(one) as archive:
                item = archive.getmember('source.c')
                self.assertEqual((item.uid, item.gid, item.uname, item.gname, item.mode, item.mtime),
                                 (0, 0, '', '', 0o444, 1000))
            with self.assertRaises(FileExistsError): pack_tree(source, one, 1000)

    def test_links_unsafe_modes_and_excessive_inventory_fail(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            source = root/'source'; source.mkdir()
            path = source/'member'; path.write_bytes(b'content'); path.chmod(0o644)
            alias = source/'link'; alias.symlink_to(path)
            with self.assertRaises(ManifestError): pack_tree(source, root/'link.tar', 0)
            alias.unlink()
            path.chmod(0o666)
            with self.assertRaises(ManifestError): pack_tree(source, root/'mode.tar', 0)
            path.chmod(0o644)
            with self.assertRaises(ManifestError): pack_tree(source, root/'count.tar', 0, members=0)
            with self.assertRaises(ManifestError): pack_tree(source, root/'size.tar', 0, maximum=100)


if __name__ == '__main__':
    unittest.main()
