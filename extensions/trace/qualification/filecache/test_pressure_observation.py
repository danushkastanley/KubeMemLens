import json
import unittest

from verify_pressure import validate_pressure, validate_pressure_session


def records(seconds=2):
    start = 1000000000
    header = {'type': 'pressure-start', 'schemaVersion': 1, 'seconds': seconds,
              'fileBytes': 8 << 20, 'bytesPerRead': 64, 'batchReadCalls': 4096,
              'maximumReadCalls': 20000000000, 'monotonicBeforeNanos': start - 100,
              'wallNanos': 1700000000000000000, 'monotonicAfterNanos': start}
    return [header] + [{'sequence': i, 'startedMonotonicNanos': start + i * 1000000000,
                       'endedMonotonicNanos': start + (i + 1) * 1000000000,
                       'wallNanos': 1700000000000000000 + (i + 1) * 1000000000,
                       'clockAfterMonotonicNanos': start + (i + 1) * 1000000000,
                       'readCalls': 409600, 'readBytes': 409600 * 64} for i in range(seconds)]


def stream(rows):
    return ''.join(json.dumps(row) + '\n' for row in rows)


class PressureObservationTests(unittest.TestCase):
    def test_complete_contiguous_pressure_has_independent_counts(self):
        result = validate_pressure(stream(records()), 2)
        self.assertEqual(result['totalReadCalls'], 819200)
        self.assertEqual(result['operationNanos'], 2000000000)
        self.assertEqual(result['totalReadBytes'], 819200 * 64)
        self.assertEqual(validate_pressure_session('{"ready":true}\n' + stream(records()), 2), result)
        maximum = validate_pressure(stream(records(1800)), 1800)
        self.assertEqual(len(maximum['intervals']), 1800)
        self.assertNotIn('qualificationPassed', result)

    def test_missing_late_gapped_or_reordered_intervals_cannot_pass(self):
        variants = []
        rows = records()
        rows.pop()
        variants.append(rows)
        rows = records()
        rows[1]['endedMonotonicNanos'] += 100000001
        variants.append(rows)
        rows = records()
        rows[2]['startedMonotonicNanos'] += 1
        variants.append(rows)
        rows = records()
        rows[2]['sequence'] = 0
        variants.append(rows)
        rows = records()
        rows[1]['readCalls'] = 409601
        rows[1]['readBytes'] = 409601 * 64
        variants.append(rows)
        for rows in variants:
            with self.assertRaises(ValueError):
                validate_pressure(stream(rows), 2)

    def test_count_byte_privacy_and_clock_contracts_are_strict(self):
        for target, key, value in ((0, 'wallNanos', 0), (0, 'seconds', 2.0),
                (0, 'monotonicBeforeNanos', 1), (0, 'maximumReadCalls', 0),
                (1, 'wallNanos', 1700000001100000000), (1, 'clockAfterMonotonicNanos', 2100000000),
                (1, 'readBytes', 0), (1, 'readCalls', True), (1, 'path', '/private')):
            rows = records()
            rows[target][key] = value
            with self.subTest(key=key), self.assertRaises(ValueError):
                validate_pressure(stream(rows), 2)
        rows = records()
        rows[1].update(readCalls=20000002048, readBytes=20000002048 * 64)
        with self.assertRaises(ValueError):
            validate_pressure(stream(rows), 2)

    def test_duplicate_or_extra_output_and_false_readiness_fail(self):
        raw = stream(records())
        for bad in (raw.rstrip(), raw + '{}\n', raw.replace('"sequence": 0', '"sequence": 0, "sequence": 0')):
            with self.assertRaises(ValueError):
                validate_pressure(bad, 2)
        for ready in ('{"ready":1}', '{"ready":false}', '[]', '{"ready":true,"extra":1}'):
            with self.assertRaises(ValueError):
                validate_pressure_session(ready + '\n' + raw, 2)


if __name__ == '__main__':
    unittest.main()
