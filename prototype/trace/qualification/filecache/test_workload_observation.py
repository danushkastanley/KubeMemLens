"""Reject missing or inconsistent workload reference evidence."""
import unittest

from verify_workload import FILE_BYTES, validate_observation


class WorkloadObservationTests(unittest.TestCase):
    def observation(self):
        return {"mode": "cached", "fileBytes": FILE_BYTES, "pageBytes": 4096,
                "residentPagesBefore": FILE_BYTES // 4096,
                "residentPagesAfter": FILE_BYTES // 4096, "readBytes": FILE_BYTES,
                "writeBytes": 0, "operationStartedMonotonicNanos": 1_000_000_000,
                "operationEndedMonotonicNanos": 1_002_000_000,
                "operationNanos": 2_000_000}

    def test_complete_reference_is_retained_without_mutation(self):
        data = self.observation()
        before = dict(data)
        validate_observation(data, "cached")
        self.assertEqual(data, before)

    def test_missing_reversed_or_fabricated_clock_is_rejected(self):
        changes = [{"operationNanos": None}, {"operationNanos": True},
                   {"operationNanos": 2_000_001},
                   {"operationEndedMonotonicNanos": 999_000_000},
                   {"operationStartedMonotonicNanos": 0},
                   {"operationEndedMonotonicNanos": 11_000_000_000,
                    "operationNanos": 10_000_000_000}]
        for change in changes:
            with self.subTest(change=change), self.assertRaises(ValueError):
                validate_observation(self.observation() | change, "cached")

    def test_io_cache_and_disclosure_contracts_remain_strict(self):
        for change in ({"readBytes": FILE_BYTES - 1}, {"residentPagesBefore": 0},
                       {"pageBytes": 8192}, {"mode": "write"},
                       {"path": "unapproved"}):
            with self.subTest(change=change), self.assertRaises(ValueError):
                validate_observation(self.observation() | change, "cached")


if __name__ == "__main__":
    unittest.main()
