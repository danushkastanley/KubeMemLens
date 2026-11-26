"""Require complete timing and I/O evidence for persistent workload schedules."""
import copy
import json
import unittest

from verify_series import validate_series
import test_workload_observation


class SeriesTests(unittest.TestCase):
    def records(self):
        start = {"type": "series-start", "schemaVersion": 1, "count": 2,
                 "periodNanos": 100000000, "monotonicBeforeNanos": 1000000000,
                 "wallNanos": 1700000000000000000, "monotonicAfterNanos": 1000000001,
                 "firstDueNanos": 6000000001}
        rows = [start]
        for index in range(2):
            due = start["firstDueNanos"] + index * start["periodNanos"]
            observation = test_workload_observation.WorkloadObservationTests().observation()
            observation.update(operationStartedMonotonicNanos=due + 1000,
                               operationEndedMonotonicNanos=due + 2001000)
            rows.append({"sequence": index, "dueMonotonicNanos": due,
                         "observation": observation})
        return rows

    def encode(self, rows):
        return "".join(json.dumps(row) + "\n" for row in rows)

    def test_complete_schedule_is_retained(self):
        rows = self.records()
        self.assertEqual(validate_series(self.encode(rows), "cached", 2, 100), rows)

    def test_missing_duplicate_or_changed_schedule_fails(self):
        original = self.records()
        variants = [original[:-1], original + [original[-1]],
                    [original[0], original[2], original[1]]]
        for key, value in (("count", 3), ("periodNanos", 200000000),
                           ("schemaVersion", True), ("wallNanos", 0),
                           ("firstDueNanos", 7000000001),
                           ("monotonicBeforeNanos", 1), ("privatePath", "hidden")):
            rows = copy.deepcopy(original)
            rows[0][key] = value
            variants.append(rows)
        for rows in variants:
            with self.subTest(rows=rows), self.assertRaises(ValueError):
                validate_series(self.encode(rows), "cached", 2, 100)

    def test_wrong_or_late_observation_fails(self):
        for change in ({"sequence": True}, {"sequence": 0},
                       {"dueMonotonicNanos": 1}, {"observation": None}):
            rows = self.records()
            rows[2].update(change)
            with self.subTest(change=change), self.assertRaises(ValueError):
                validate_series(self.encode(rows), "cached", 2, 100)
        for offset in (-1, 98000000):
            rows = self.records()
            due = rows[1]["dueMonotonicNanos"]
            rows[1]["observation"].update(operationStartedMonotonicNanos=due + offset,
                                           operationEndedMonotonicNanos=due + offset + 2000000)
            with self.subTest(offset=offset), self.assertRaises(ValueError):
                validate_series(self.encode(rows), "cached", 2, 100)

    def test_raw_encoding_does_not_hide_ambiguity_or_extra_fields(self):
        raw = self.encode(self.records())
        for invalid in (raw[:-1], raw + "\n", raw.replace('"count": 2', '"count": 2, "count": 2'),
                        raw.replace('"readBytes": 8388608', '"readBytes": 0'),
                        raw.replace('"writeBytes": 0', '"writeBytes": false'), "x" * (2 * 1024 * 1024 + 1)):
            with self.assertRaises(ValueError):
                validate_series(invalid, "cached", 2, 100)

    def test_expected_bounds_are_validated(self):
        for mode, count, period in (("prepare", 2, 100), ("cached", True, 100),
                                    ("cached", 1801, 100), ("cached", 2, 99),
                                    ("cached", 2, 10001), ("cached", 1800, 1001)):
            with self.subTest(mode=mode, count=count, period=period), self.assertRaises(ValueError):
                validate_series(self.encode(self.records()), mode, count, period)


if __name__ == "__main__":
    unittest.main()
