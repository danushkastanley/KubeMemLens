import unittest

from activity import active_intervals, validate_witness
from test_samples import fixture


COUNTS = {"map": 8, "prog": 5, "link": 5}


def witness(seconds=5):
    rows = []
    wall = 1700000000000000000
    for index in range(seconds + 1):
        point = {"monotonicNanos": 1000000000 + index * 1000000000,
                 "wallNanos": wall + index * 1000000000, "uncertaintyNanos": 1000}
        snap = {"workers": 1, "excludedWorkers": 0, "activeControls": 1,
                "objects": {kind: list(range(1, count + 1)) for kind, count in COUNTS.items()},
                "kernelMapBytes": 1000000, "userMapBytes": 540672, "clock": point}
        rows.append({"schemaVersion": 1, "index": index, "elapsedNanos": index * 1000000000,
                     "readNanos": 1000000, "state": "observed", "snapshot": snap,
                     "clock": dict(point), "observerCPUUsec": 1000 * index,
                     "observerPeakRSSBytes": 20 << 20})
    return rows


class ActivityTests(unittest.TestCase):
    def test_outer_intervals_lack_brackets_and_remain_unproven(self):
        mask = active_intervals(fixture(5), witness(), 5, COUNTS)
        self.assertEqual(mask, [False, True, True, True, False])

    def test_unknown_partial_or_replaced_attachments_are_never_bridged(self):
        for state in ("unavailable", "partial", "new-links", "excluded", "inactive"):
            rows = witness()
            if state == "unavailable":
                rows[2]["state"] = "unavailable"
                del rows[2]["snapshot"]
            elif state == "partial":
                rows[2]["snapshot"]["objects"]["link"].pop()
            elif state == "new-links":
                rows[2]["snapshot"]["objects"]["link"] = [100, 101, 102, 103, 104]
            elif state == "excluded":
                rows[2]["snapshot"]["excludedWorkers"] = 1
            else:
                rows[2]["snapshot"]["activeControls"] = 0
            mask = active_intervals(fixture(5), rows, 5, COUNTS)
            with self.subTest(state=state):
                self.assertFalse(any(mask[:4]))

    def test_malformed_witness_cannot_claim_activity(self):
        for change in ("duplicate", "missing", "unknown-zero", "clock", "monotonic", "read", "order"):
            rows = witness()
            if change == "duplicate":
                rows[1]["snapshot"]["objects"]["map"].append(1)
            elif change == "missing":
                rows.pop()
            elif change == "unknown-zero":
                rows[1]["state"] = "unavailable"
            elif change == "clock":
                rows[1]["clock"]["wallNanos"] += 500000000
            elif change == "monotonic":
                rows[1]["clock"]["monotonicNanos"] += 500000000
            elif change == "read":
                rows[1]["readNanos"] = 100000001
            else:
                rows[1]["index"] = 2
            with self.subTest(change=change), self.assertRaises(ValueError):
                validate_witness(rows, 5)

    def test_long_resource_reads_require_a_later_witness(self):
        rows = fixture(5)
        for row in rows:
            row["wallNanos"] += 500000000
            row["readNanos"] = 100000000
        changed = witness()
        changed[3]["snapshot"]["objects"]["prog"] = [100, 101, 102, 103, 104]
        self.assertFalse(active_intervals(rows, changed, 5, COUNTS)[1])


if __name__ == "__main__":
    unittest.main()
