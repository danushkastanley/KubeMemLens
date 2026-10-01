import copy
import unittest

from flood_resources import flood_resources, validate_containment_window
from samples import validate_window
from test_samples import fixture

ROLES = {'node', 'api'}
CAPS = {role: {'cpuQuotaUsec': 200000, 'cpuPeriodUsec': 100000,
               'cpuBurstUsec': 0, 'memoryMaxBytes': 512 << 20} for role in ROLES}


def contained():
    rows = fixture()
    for row in rows:
        row['schemaVersion'] = 3
        for group in row['groups'].values():
            group['containment'] = {
                'cpu': {'maximum': {'state': 'observed', 'value': 200000}, 'periodUsec': 100000},
                'cpuBurstUsec': {'state': 'observed', 'value': 0},
                'memoryMaxBytes': {'state': 'observed', 'value': 512 << 20},
                'memoryPeakBytes': {'state': 'observed', 'value': 25 << 20},
                'pidsMax': {'state': 'unlimited'},
            }
    return rows


class FloodResourcesTests(unittest.TestCase):
    def test_explicit_format_retains_limits_and_lifetime_peak(self):
        rows = contained()
        original = copy.deepcopy(rows)
        result = flood_resources(rows, 3, ROLES, CAPS)
        self.assertTrue(result['observedContainmentPassed'])
        self.assertEqual(rows, original)
        self.assertEqual(result['services']['node']['memoryLifetimePeakBytes'], 25 << 20)
        self.assertEqual(result['services']['node']['configuredCaps']['pidsMax'], {'state': 'unlimited'})
        self.assertNotIn('normalCPUBudgetPassed', result)
        with self.assertRaises(ValueError):
            validate_window(rows, 3, ROLES)
        with self.assertRaises(ValueError):
            validate_containment_window(fixture(), 3, ROLES)

    def test_unknown_peak_is_not_zero_or_a_pass(self):
        rows = contained()
        rows[1]['groups']['node']['containment']['memoryPeakBytes'] = {'state': 'unavailable'}
        result = flood_resources(rows, 3, ROLES, CAPS)
        self.assertFalse(result['observedContainmentPassed'])
        self.assertIsNone(result['services']['node']['memoryLifetimePeakBytes'])
        rows[1]['groups']['node']['containment']['memoryPeakBytes']['value'] = 0
        with self.assertRaises(ValueError):
            flood_resources(rows, 3, ROLES, CAPS)

    def test_exceeded_memory_or_mismatched_cpu_is_retained_as_failure(self):
        for field, value in [('memoryPeakBytes', 513 << 20), ('cpuBurstUsec', 1000)]:
            rows = contained()
            for row in rows:
                row['groups']['node']['containment'][field]['value'] = value
            self.assertFalse(flood_resources(rows, 3, ROLES, CAPS)['observedContainmentPassed'])
        rows = contained()
        for row in rows:
            row['groups']['api']['containment']['cpu']['maximum'] = {'state': 'unlimited'}
        self.assertFalse(flood_resources(rows, 3, ROLES, CAPS)['observedContainmentPassed'])

    def test_caps_cannot_change_and_known_peaks_cannot_reset(self):
        for field, value in [('memoryPeakBytes', 24 << 20), ('memoryMaxBytes', 256 << 20)]:
            rows = contained()
            rows[1]['groups']['node']['containment'][field]['value'] = value
            with self.subTest(field=field), self.assertRaises(ValueError):
                flood_resources(rows, 3, ROLES, CAPS)

    def test_unavailable_pids_fails_and_numeric_max_is_enforced(self):
        for limit in ({'state': 'unavailable'}, {'state': 'observed', 'value': 1}):
            rows = contained()
            for row in rows:
                row['groups']['api']['containment']['pidsMax'] = limit
            self.assertFalse(flood_resources(rows, 3, ROLES, CAPS)['observedContainmentPassed'])

    def test_cadence_oom_and_private_fields_still_fail(self):
        cases = []
        rows = contained()
        rows[1]['groups']['node']['memoryEvents']['oom'] = 1
        cases.append(rows)
        rows = contained()
        rows[1]['elapsedNanos'] += 200000000
        cases.append(rows)
        rows = contained()
        rows[1]['groups']['node']['containment']['path'] = '/private'
        cases.append(rows)
        for rows in cases:
            with self.assertRaises(ValueError):
                flood_resources(rows, 3, ROLES, CAPS)


if __name__ == '__main__':
    unittest.main()
