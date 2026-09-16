"""Synthetic complete bundle plus provenance, lifetime and cleanup negatives."""

from datetime import datetime, timedelta, timezone
import json
from pathlib import Path
import shutil
import tempfile
import unittest
from copy import deepcopy
from unittest.mock import patch

from idle_campaign import PROFILE, ROOT, source_files, source_manifest
from idle_evaluate import EXPECTED_PROFILE, evaluate_pair
from local_runtime import canonical, digest
from replay_idle import replay
from test_idle_evaluate import window


def save(path, value):
    path.write_text(json.dumps(value))


class ReplayTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        source = source_manifest()
        for path in source_files():
            target = self.root / "source" / path.relative_to(ROOT)
            target.parent.mkdir(parents=True, exist_ok=True)
            shutil.copyfile(path, target)
        frozen = {"profile": EXPECTED_PROFILE, "source": source, "sourceSHA256": digest(canonical(source)),
                  "profileSHA256": digest(PROFILE.read_bytes()), "image": "example@sha256:" + "a" * 64,
                  "frozenAt": "2026-09-16T00:00:00+00:00"}
        save(self.root / "freeze.json", frozen)
        self.freeze_pin = digest((self.root / "freeze.json").read_bytes())
        now = datetime(2026, 9, 16, tzinfo=timezone.utc)
        transitions = []
        for pair in range(1, 6):
            windows = {}
            for phase in ("control", "enabled"):
                transitions.append({"pair": pair, "phase": "start" if phase == "enabled" else "stop",
                                    "startedAt": now.isoformat(), "completedAt": now.isoformat()})
                now += timedelta(seconds=60)
                started = now
                now += timedelta(seconds=900)
                rows = window(phase == "enabled")
                start_nanos = int(started.timestamp()) * 10**9
                for row in rows:
                    row["wallNanos"] = start_nanos + row["elapsedNanos"]
                stem = f"pair-{pair}-{phase}"
                path = self.root / (stem + ".jsonl")
                path.write_text("".join(json.dumps(r) + "\n" for r in rows))
                proof = {role: {"identity": digest(f"{pair}:{role}".encode()), "snapshot": {
                    "workers": 0, "excludedWorkers": 0, "activeControls": 0,
                    "objects": {"link": [], "map": [], "prog": []}, "kernelMapBytes": 0,
                    "userMapBytes": 0, "clock": {"monotonicNanos": 1, "wallNanos": start_nanos - 1_000_000, "uncertaintyNanos": 1}}}
                         for role in ("node", "api")} if phase == "enabled" else {}
                after = deepcopy(proof)
                for value in after.values():
                    value["snapshot"]["clock"]["wallNanos"] = start_nanos + 900 * 10**9 + 1_000_000
                envelope = {"startedAt": started.isoformat(), "completedAt": now.isoformat(), "phase": phase,
                            "pair": pair, "samplesSHA256": digest(path.read_bytes()), "image": frozen["image"],
                            "sourceSHA256": frozen["sourceSHA256"], "before": proof, "after": after,
                            "optionalServicesAbsent": phase == "control"}
                save(self.root / (stem + ".envelope.json"), envelope)
                windows[phase] = rows
            result = evaluate_pair(windows["control"], windows["enabled"])
            result["pair"] = pair
            save(self.root / f"pair-{pair}-result.json", result)
        save(self.root / "transitions.json", transitions)

    def checked_replay(self, source_pin=None):
        return replay(self.root, source_pin, expected_freeze_sha256=self.freeze_pin)

    def test_complete_bundle_recomputes_all_five_pairs(self):
        result = self.checked_replay()
        self.assertTrue(result["idleBudgetPassed"])
        self.assertEqual(len(result["pairs"]), 5)
        self.assertIn("remaining gates not established", result["qualification"])

    def test_altered_candidate_lifetime_cleanup_and_raw_data_rejected(self):
        path = self.root / "pair-1-enabled.envelope.json"
        original = json.loads(path.read_text())
        for mutate in (
            lambda e: e.update(image="example@sha256:" + "c" * 64),
            lambda e: e["after"]["node"].update(identity="c" * 64),
            lambda e: e["after"]["node"]["snapshot"].update(excludedWorkers=1),
            lambda e: e["after"]["node"]["snapshot"]["objects"].update(map=[123]),
            lambda e: e.update(samplesSHA256="c" * 64),
        ):
            value = json.loads(json.dumps(original))
            mutate(value)
            save(path, value)
            with self.assertRaises(ValueError):
                self.checked_replay()

    def test_short_warmup_and_missing_fifth_pair_rejected(self):
        path = self.root / "transitions.json"
        value = json.loads(path.read_text())
        value[0]["completedAt"] = "2026-09-16T00:00:01+00:00"
        save(path, value)
        with self.assertRaises(ValueError):
            self.checked_replay()
        value[0]["completedAt"] = "2026-09-16T00:00:00+00:00"
        save(path, value[:-1])
        with self.assertRaises(ValueError):
            self.checked_replay()

    def test_relabelled_raw_window_is_rejected(self):
        original = (self.root / "pair-1-enabled.jsonl").read_bytes()
        target = self.root / "pair-2-enabled.jsonl"
        target.write_bytes(original)
        envelope_path = self.root / "pair-2-enabled.envelope.json"
        envelope = json.loads(envelope_path.read_text())
        envelope["samplesSHA256"] = digest(original)
        save(envelope_path, envelope)
        with self.assertRaisesRegex(ValueError, "raw window reused"):
            self.checked_replay()

    def test_changed_raw_epoch_cannot_fit_an_unrelated_envelope(self):
        target = self.root / "pair-2-enabled.jsonl"
        rows = [json.loads(line) for line in target.read_text().splitlines()]
        for row in rows:
            row["wallNanos"] -= 60 * 10**9
        target.write_text("".join(json.dumps(r) + "\n" for r in rows))
        envelope_path = self.root / "pair-2-enabled.envelope.json"
        envelope = json.loads(envelope_path.read_text())
        envelope["samplesSHA256"] = digest(target.read_bytes())
        save(envelope_path, envelope)
        with self.assertRaisesRegex(ValueError, "raw epoch"):
            self.checked_replay()

    def test_replacement_requires_new_service_lifetimes(self):
        first = json.loads((self.root / "pair-1-enabled.envelope.json").read_text())
        path = self.root / "pair-2-enabled.envelope.json"
        second = json.loads(path.read_text())
        for when in ("before", "after"):
            second[when]["node"]["identity"] = first[when]["node"]["identity"]
        save(path, second)
        with self.assertRaisesRegex(ValueError, "service lifetime reused"):
            self.checked_replay()

    def test_historical_source_requires_an_independent_explicit_pin(self):
        pin = json.loads((self.root / "freeze.json").read_text())["sourceSHA256"]
        with patch("replay_idle.source_manifest", return_value={"revision": "new verifier"}):
            with self.assertRaisesRegex(ValueError, "source does not match"):
                self.checked_replay()
            result = self.checked_replay(pin)
            self.assertEqual(result["measurementSourceSHA256"], pin)
            self.assertNotEqual(result["replaySourceSHA256"], pin)

    def test_stale_after_census_is_rejected(self):
        path = self.root / "pair-1-enabled.envelope.json"
        value = json.loads(path.read_text())
        value["after"]["node"]["snapshot"]["clock"] = value["before"]["node"]["snapshot"]["clock"]
        save(path, value)
        with self.assertRaisesRegex(ValueError, "census does not bracket"):
            self.checked_replay()

    def test_consistently_substituted_candidate_cannot_replace_the_receipt(self):
        path = self.root / "freeze.json"
        frozen = json.loads(path.read_text())
        frozen["image"] = "different@sha256:" + "c" * 64
        save(path, frozen)
        for path in self.root.glob("*.envelope.json"):
            envelope = json.loads(path.read_text())
            envelope["image"] = frozen["image"]
            save(path, envelope)
        with self.assertRaisesRegex(ValueError, "candidate metadata pin mismatch"):
            self.checked_replay()


if __name__ == "__main__":
    unittest.main()
