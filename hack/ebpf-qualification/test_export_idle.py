"""Public export retains complete evidence and rejects unreviewed payloads."""

import gzip
import json
from pathlib import Path
import unittest

from export_idle import bounded, export, numeric_samples
from local_runtime import digest
from idle_evaluate import evaluate_pair
from verify_idle_export import verify
import test_replay_idle as fixtures


class ExportTests(unittest.TestCase):
    def setUp(self):
        self.fixture = fixtures.ReplayTests("test_complete_bundle_recomputes_all_five_pairs")
        self.fixture.setUp()
        self.addCleanup(self.fixture.doCleanups)
        self.root = self.fixture.root
        frozen_path = self.root / "freeze.json"
        frozen = json.loads(frozen_path.read_text())
        for field in ("nodeSHA256", "workerSHA256", "measureSHA256", "censusSHA256", "policySHA256",
                      "engineSHA256", "programmeIndexSHA256", "privateConfigurationSHA256"):
            frozen[field] = digest(field.encode())
        frozen["candidateCommit"] = "a" * 40
        frozen["environment"] = {"kernelVersion": "7.0.12-linuxkit", "osImage": "Debian GNU/Linux 13 (trixie)",
                                  "containerRuntimeVersion": "containerd://2.3.4", "kubeletVersion": "v1.37.0",
                                  "architecture": "arm64", "operatingSystem": "linux", "sharedKindKernel": True}
        fixtures.save(frozen_path, frozen)
        self.pin = frozen["sourceSHA256"]
        self.freeze_pin = digest(frozen_path.read_bytes())
        for pair in range(1, 6):
            for phase in ("control", "enabled"):
                (self.root / f"pair-{pair}-{phase}.stderr").write_bytes(b"")

    def test_complete_export_is_byte_preserving_and_excludes_private_files(self):
        (self.root / "private-runtime.json").write_text('{"private":"not-for-public"}')
        output = self.root / "public"
        manifest = export(self.root, output, self.pin, self.freeze_pin)
        self.assertEqual(manifest["windowCount"], 10)
        self.assertNotIn("private-runtime.json", manifest["files"])
        for name, entry in manifest["files"].items():
            encoded = (output / name).read_bytes()
            self.assertEqual(digest(encoded), entry["sha256"])
            raw = gzip.decompress(encoded) if entry["encoding"] == "gzip" else encoded
            self.assertEqual(digest(raw), entry["rawSHA256"])
            if name.endswith(".jsonl.gz"):
                self.assertEqual(raw, (self.root / name.removesuffix(".gz")).read_bytes())
        with self.assertRaisesRegex(ValueError, "already exists"):
            export(self.root, output, self.pin, self.freeze_pin)
        self.assertTrue(verify(output, self.pin, self.freeze_pin)["idleBudgetPassed"])

    def test_complete_failed_run_remains_failed_in_export_and_replay(self):
        target = self.root / "pair-1-enabled.jsonl"
        rows = [json.loads(line) for line in target.read_text().splitlines()]
        rows[450]["groups"]["node"]["memoryCurrent"] += 1
        target.write_text("".join(json.dumps(row) + "\n" for row in rows))
        path = self.root / "pair-1-enabled.envelope.json"
        envelope = json.loads(path.read_text())
        envelope["samplesSHA256"] = digest(target.read_bytes())
        fixtures.save(path, envelope)
        control = [json.loads(line) for line in (self.root / "pair-1-control.jsonl").read_text().splitlines()]
        result = evaluate_pair(control, rows)
        result["pair"] = 1
        fixtures.save(self.root / "pair-1-result.json", result)
        output = self.root / "public"
        export(self.root, output, self.pin, self.freeze_pin)
        replayed = verify(output, self.pin, self.freeze_pin)
        self.assertFalse(replayed["idleBudgetPassed"])
        self.assertFalse(replayed["pairs"][0]["idleBudgetPassed"])
        self.assertEqual(len(replayed["pairs"]), 5)

    def test_corrupted_or_redirected_export_is_rejected(self):
        output = self.root / "public"
        export(self.root, output, self.pin, self.freeze_pin)
        target = output / "pair-1-control.jsonl.gz"
        target.write_bytes(target.read_bytes() + b"tampered")
        with self.assertRaises(ValueError):
            verify(output, self.pin, self.freeze_pin)
        path = output / "manifest.json"
        manifest = json.loads(path.read_text())
        manifest["files"]["../outside"] = manifest["files"].pop("pair-1-control.jsonl.gz")
        fixtures.save(path, manifest)
        with self.assertRaises(ValueError):
            verify(output, self.pin, self.freeze_pin)

    def test_incomplete_campaign_cannot_export(self):
        (self.root / "pair-5-enabled.envelope.json").unlink()
        with self.assertRaises(ValueError):
            export(self.root, self.root / "public", self.pin, self.freeze_pin)
        self.assertFalse((self.root / "public").exists())

    def test_extra_counter_and_diagnostics_need_review(self):
        rows = [json.loads(line) for line in (self.root / "pair-1-enabled.jsonl").read_text().splitlines()]
        rows[0]["groups"]["node"]["memory"]["unreviewed_counter"] = 1
        raw = b"".join(json.dumps(r).encode() + b"\n" for r in rows)
        with self.assertRaisesRegex(ValueError, "unreviewed counter"):
            numeric_samples(raw, True)
        (self.root / "pair-1-control.stderr").write_text("requires inspection")
        with self.assertRaisesRegex(ValueError, "diagnostics require"):
            export(self.root, self.root / "public", self.pin, self.freeze_pin)

    def test_shadowed_duplicate_field_cannot_be_published(self):
        original = (self.root / "pair-1-enabled.jsonl").read_bytes()
        changed = original.replace(b'"memoryCurrent": ', b'"memoryCurrent":"unreviewed","memoryCurrent": ', 1)
        self.assertNotEqual(original, changed)
        with self.assertRaisesRegex(ValueError, "duplicate evidence"):
            numeric_samples(changed, True)

    def test_private_metadata_field_is_rejected(self):
        path = self.root / "freeze.json"
        value = json.loads(path.read_text())
        value["privateRuntimePath"] = "/private/example"
        fixtures.save(path, value)
        self.freeze_pin = digest(path.read_bytes())
        with self.assertRaisesRegex(ValueError, "unexpected evidence fields"):
            export(self.root, self.root / "public", self.pin, self.freeze_pin)

    def test_symlinked_or_oversized_file_is_not_read_as_evidence(self):
        path = self.root / "link"
        path.symlink_to(self.root / "freeze.json")
        with self.assertRaises(ValueError):
            bounded(path, 512 * 1024)
        with self.assertRaises(ValueError):
            bounded(self.root / "freeze.json", 2)


if __name__ == "__main__":
    unittest.main()
