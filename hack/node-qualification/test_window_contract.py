import copy
import unittest

from common import ContractError
from window_contract import join_windows, validate_window
from test_contract import fixture, version_two


class WindowContractTest(unittest.TestCase):
    def setUp(self):
        self.profile, old = fixture()
        self.evidence = version_two(self.profile, old)
        self.windows = []
        for phase, start, end in (("baseline", "12:00:00", "12:03:00"), ("enabled", "12:03:00", "12:15:00")):
            self.windows.append({"schemaVersion": 2, "phase": phase, "profile": self.evidence["profile"],
                "observation": self.evidence["observation"], "startedAt": "2026-09-11T" + start + "Z",
                "completedAt": "2026-09-11T" + end + "Z", "nodes": [{"slot": 0,
                "samples": self.evidence["nodes"][0]["samples"][phase], "rotation": self.evidence["nodes"][0]["rotation"]}]})

    def test_join_preserves_the_observed_samples_and_rotation(self):
        result = join_windows(self.profile, *self.windows)
        self.assertEqual(result["nodes"], self.evidence["nodes"])
        self.assertEqual(result["observation"], self.evidence["observation"])

    def test_mixed_profiles_phases_protocols_or_overlapping_times_are_rejected(self):
        for field, value in (("profile", {"id": "different", "digest": "sha256:" + "a" * 64}),
                             ("phase", "baseline"), ("schemaVersion", 1),
                             ("observation", {"method": "docker", "image": "different"}),
                             ("startedAt", "2026-09-11T12:02:00Z")):
            with self.subTest(field=field):
                changed = copy.deepcopy(self.windows)
                changed[1][field] = value
                with self.assertRaises(ContractError):
                    join_windows(self.profile, *changed)

    def test_private_fields_never_enter_the_shared_window(self):
        self.windows[0]["nodes"][0]["nodeUID"] = "private-identity"
        with self.assertRaises(ContractError):
            validate_window(self.profile, self.windows[0], "baseline")


if __name__ == "__main__":
    unittest.main()
