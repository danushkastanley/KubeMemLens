import hashlib
import io
import subprocess
import tarfile
import unittest

from common import ContractError
from prepare_provider import REPOSITORY
from source_digest import production_source


class SourceDigestTest(unittest.TestCase):
    def test_approved_commit_matches_the_existing_source_protocol(self):
        commit = subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=REPOSITORY, text=True).strip()
        data = subprocess.check_output(["git", "archive", commit, "go.mod", "go.sum", "cmd", "internal"], cwd=REPOSITORY)
        expected = hashlib.sha256()
        with tarfile.open(fileobj=io.BytesIO(data), mode="r:") as archive:
            files = [m for m in archive if m.isfile() and (m.name in {"go.mod", "go.sum"}
                     or m.name.endswith(".go") and not m.name.endswith("_test.go"))]
            for member in sorted(files, key=lambda m: m.name):
                expected.update(member.name.encode() + b"\0" + archive.extractfile(member).read() + b"\0")
        self.assertEqual(production_source(commit), "sha256:" + expected.hexdigest())

    def test_missing_or_changed_blob_is_rejected(self):
        oid = b"a" * 40
        listing = b"100644 blob " + oid + b" 1\tgo.mod\0" + b"100644 blob " + oid + b" 1\tgo.sum\0"
        for output in ("missing\n", "a" * 40 + " blob 2\nx\n"):
            with self.assertRaises(ContractError):
                production_source("b" * 40, command=lambda *_: listing, run=lambda *_, **__: output)

    def test_abbreviated_commit_is_rejected_before_git(self):
        with self.assertRaises(ContractError):
            production_source("abc")


if __name__ == "__main__":
    unittest.main()
