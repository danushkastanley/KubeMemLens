import copy
import json
from pathlib import Path
import tempfile
import unittest

from profile import EXPECTED, load_profile, trace_slots


class ProfileTests(unittest.TestCase):
    def test_schedule_fits_admission_and_preserves_required_active_time(self):
        p = load_profile()
        slots = trace_slots(p)
        self.assertEqual(len(slots), 36)
        self.assertEqual(slots[0], 30)
        self.assertLess(slots[-1] + p['trace']['durationSeconds'], p['windowSeconds'])
        self.assertEqual(p['trace']['durationSeconds'], 30)
        self.assertGreaterEqual(36 * 30, p['minimumActiveSeconds'])
        self.assertLess(5 + p['workload']['count'] * p['workload']['periodMilliseconds'] / 1000,
                        p['windowSeconds'])

    def test_changed_limits_shortened_case_or_wrong_type_fail(self):
        for field, value in [('pairs', 1), ('windowSeconds', 30), ('minimumActiveSeconds', 899),
                             ('schemaVersion', True), ('minimumScans', 240.0)]:
            p = copy.deepcopy(EXPECTED)
            p[field] = value
            with tempfile.TemporaryDirectory() as directory:
                path = Path(directory) / 'profile.json'
                path.write_text(json.dumps(p))
                with self.subTest(field=field), self.assertRaises(ValueError):
                    load_profile(path)


if __name__ == '__main__':
    unittest.main()
