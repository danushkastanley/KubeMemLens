"""A high-rate series must retain every operation and its fixed mixed order."""
import copy
import json
import unittest

from verify_series import MIXED_MODES, validate_mixed_series, validate_series
from verify_workload import expected_observation


def records(count=6, period_ms=100):
    start = {"type": "mixed-series-start", "schemaVersion": 2, "count": count,
             "periodNanos": period_ms * 1000000, "monotonicBeforeNanos": 1000000000,
             "wallNanos": 1700000000000000000, "monotonicAfterNanos": 1000000001,
             "firstDueNanos": 6000000001}
    rows = [start]
    for index in range(count):
        due = start["firstDueNanos"] + index * start["periodNanos"]
        observation = expected_observation({"pageBytes": 4096}, MIXED_MODES[index % 3])
        observation.update(operationStartedMonotonicNanos=due + 1000,
                           operationEndedMonotonicNanos=due + 1001000, operationNanos=1000000)
        rows.append({"sequence": index, "dueMonotonicNanos": due, "observation": observation})
    return rows


def encode(rows):
    return "".join(json.dumps(row) + "\n" for row in rows)


class MixedSeriesTests(unittest.TestCase):
    def test_every_cycle_retains_read_write_and_residency_evidence(self):
        rows = records()
        self.assertEqual(validate_mixed_series(encode(rows), 6, 100), rows)
        self.assertEqual([row['observation']['mode'] for row in rows[1:]], list(MIXED_MODES) * 2)

    def test_full_thirty_minute_schedule_fits_explicit_output_bound(self):
        rows = records(18000)
        raw = encode(rows)
        self.assertLess(len(raw.encode()), 8 * 1024 * 1024)
        self.assertEqual(len(validate_mixed_series(raw, 18000, 100)), 18001)

    def test_mixed_records_cannot_be_interpreted_as_normal_series(self):
        raw = encode(records())
        with self.assertRaises(ValueError):
            validate_series(raw, 'cached', 6, 100)
        for key, value in (('type', 'series-start'), ('schemaVersion', 1), ('schemaVersion', True)):
            rows = records()
            rows[0][key] = value
            with self.subTest(key=key, value=value), self.assertRaises(ValueError):
                validate_mixed_series(encode(rows), 6, 100)

    def test_substitution_truncation_or_reordered_evidence_fails(self):
        original = records()
        variants = [original[:-1], original + [original[-1]],
                    [original[0], original[2], original[1], *original[3:]]]
        for index, key, value in ((2, 'mode', 'cached'), (2, 'residentPagesBefore', 2048),
                                  (3, 'writeBytes', 0), (1, 'readBytes', True),
                                  (1, 'operationNanos', 999999)):
            rows = copy.deepcopy(original)
            rows[index]['observation'][key] = value
            variants.append(rows)
        for rows in variants:
            with self.subTest(rows=rows), self.assertRaises(ValueError):
                validate_mixed_series(encode(rows), 6, 100)

    def test_absolute_deadline_is_exclusive_even_with_consistent_elapsed_time(self):
        rows = records()
        obs = rows[1]['observation']
        obs['operationEndedMonotonicNanos'] = rows[1]['dueMonotonicNanos'] + rows[0]['periodNanos']
        obs['operationNanos'] = obs['operationEndedMonotonicNanos'] - obs['operationStartedMonotonicNanos']
        with self.assertRaisesRegex(ValueError, 'series deadline missed'):
            validate_mixed_series(encode(rows), 6, 100)

    def test_invalid_schedule_or_ambiguous_raw_output_fails(self):
        raw = encode(records())
        for count, period in ((0, 100), (True, 100), (5, 100), (18003, 100),
                              (6, 99), (6, True), (6, 10001), (18000, 101)):
            with self.subTest(count=count, period=period), self.assertRaises(ValueError):
                validate_mixed_series(raw, count, period)
        for invalid in (raw[:-1], raw + '\n', raw.replace('"count": 6', '"count": 6, "count": 6'),
                        raw.replace('"writeBytes": 0', '"writeBytes": 0, "privatePath": "hidden"'),
                        'x' * (8 * 1024 * 1024 + 1)):
            with self.assertRaises(ValueError):
                validate_mixed_series(invalid, 6, 100)


if __name__ == '__main__':
    unittest.main()
