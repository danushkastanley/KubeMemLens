import copy
import unittest

from flood_result import kernel_budget


def evidence():
    return {'transportComplete': True, 'termination': 'expired', 'requestedCeilingObserved': False,
            'events': 2929, 'engineCounts': {'produced': 131072, 'sampled': 121072, 'lost': 7071, 'rejected': 0}}


class KernelBudgetTests(unittest.TestCase):
    def test_ring_loss_can_exhaust_kernel_budget_without_a_writer_limit(self):
        value = evidence()
        result = kernel_budget(value, {'mode': 'flood', 'readCalls': 131072}, 10000)
        self.assertTrue(result['kernelBudgetAndLossPassed'])
        self.assertFalse(value['requestedCeilingObserved'])
        self.assertEqual(result['admittedCandidates'], 10000)

    def test_unknown_counters_cannot_be_replaced_with_zero(self):
        for key in evidence()['engineCounts']:
            value = evidence()
            value['engineCounts'][key] = None
            with self.subTest(key=key), self.assertRaises(ValueError):
                kernel_budget(value, {'mode': 'flood', 'readCalls': 131072}, 10000)

    def test_no_loss_wrong_scope_or_unaccounted_budget_remains_non_pass(self):
        for key, change in [('lost', 0), ('sampled', 121071), ('produced', 131073), ('rejected', 1)]:
            value = copy.deepcopy(evidence())
            value['engineCounts'][key] = change
            with self.subTest(key=key):
                self.assertFalse(kernel_budget(value, {'mode': 'flood', 'readCalls': 131072}, 10000)['kernelBudgetAndLossPassed'])


if __name__ == '__main__':
    unittest.main()
