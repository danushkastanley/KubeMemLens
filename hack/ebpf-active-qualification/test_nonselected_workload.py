import json
import unittest

from nonselected_workload import ROLES, compare_nonselected
from test_workload import series
from test_activity import COUNTS, witness


def noise(elapsed=1000000):
    rows = [json.loads(line) for line in series(elapsed).splitlines()]
    for row in rows[1:]:
        row['observation'].update(mode='noise', readBytes=33554432, writeBytes=33554432)
    return ''.join(json.dumps(row) + '\n' for row in rows)


def compare(before, after, activity=None, minimum=5):
    return compare_nonselected(before, after, count=5, period_ms=1000, witness=activity or witness(12),
                               seconds=12, expected_objects=COUNTS, minimum_active_operations=minimum)


class NonselectedWorkloadTests(unittest.TestCase):
    def test_each_peer_is_retained_and_one_percent_is_an_exclusive_limit(self):
        before = {role: noise() for role in ROLES}
        for elapsed, passed in ((1009999, True), (1010000, False), (2000000, False)):
            after = dict(before)
            after['nonselected-9'] = noise(elapsed)
            with self.subTest(elapsed=elapsed):
                result = compare(before, after)
                self.assertEqual(set(result['workloads']), set(ROLES))
                self.assertEqual(result['nonselectedLatencyBudgetPassed'], passed)
                self.assertEqual(sum(v['nonselectedLatencyBudgetPassed'] for v in result['workloads'].values()), 9 + int(passed))

    def test_missing_added_or_relabelled_peer_cannot_pass(self):
        before = {role: noise() for role in ROLES}
        missing = dict(before)
        missing.pop('nonselected-9')
        changed = dict(missing, **{'nonselected-10': noise()})
        for after in (missing, changed, dict(before, extra=noise()), []):
            with self.subTest(after=type(after)), self.assertRaises(ValueError):
                compare(before, after)

    def test_partial_wrong_mode_and_shifted_clock_in_one_peer_fail(self):
        before = {role: noise() for role in ROLES}
        for raw in ('\n'.join(noise().splitlines()[:-1]) + '\n', series(),
                    noise().replace('"wallNanos": 1700000000000000000', '"wallNanos": 1700000001000000000')):
            after = dict(before)
            after['nonselected-3'] = raw
            with self.assertRaises(ValueError):
                compare(before, after)

    def test_gaps_remain_in_every_peer_and_cannot_satisfy_active_minimum(self):
        streams = {role: noise() for role in ROLES}
        activity = witness(12)
        activity[7]['snapshot']['activeControls'] = 0
        result = compare(streams, streams, activity, minimum=1)
        for value in result['workloads'].values():
            self.assertEqual(value['allOperations']['control']['operations'], 5)
            self.assertIsNotNone(value['unprovenOrTransitionOperations'])
        with self.assertRaises(ValueError):
            compare(streams, streams, activity)


if __name__ == '__main__':
    unittest.main()
