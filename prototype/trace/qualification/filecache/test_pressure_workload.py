import sys
import unittest

from pressure_workload import PressureWorkload
from test_pressure_observation import records, stream


SCRIPT = '''
import sys
print('{"ready":true}', flush=True)
assert sys.stdin.buffer.read(1) == b'R'
sys.stdout.write(sys.argv[1])
sys.stdout.flush()
assert sys.stdin.buffer.read(1) == b'Q'
'''


class PressureWorkloadTests(unittest.TestCase):
    def producer(self, raw):
        return PressureWorkload([sys.executable, '-u', '-c', SCRIPT, raw], 1)

    def test_nonblocking_start_collect_and_final_handshake(self):
        workload = self.producer(stream(records(1)))
        try:
            self.assertIsNone(workload.process.poll())
            with self.assertRaises(ValueError):
                workload.collect()
            workload.start()
            with self.assertRaises(ValueError):
                workload.start()
            receipt = workload.collect()
            self.assertEqual(receipt['totalReadCalls'], 409600)
            self.assertIsNone(workload.process.poll())
            with self.assertRaises(ValueError):
                workload.collect()
            workload.finish()
            self.assertEqual(workload.process.returncode, 0)
        finally:
            workload.close()

    def test_malformed_pressure_receipt_cannot_be_finished_as_success(self):
        workload = self.producer(stream(records(1)) + '{}\n')
        try:
            workload.start()
            with self.assertRaises(ValueError):
                workload.collect()
            with self.assertRaises(ValueError):
                workload.finish()
        finally:
            workload.close()

    def test_duration_is_checked_before_creating_a_process(self):
        for value in (0, 1801, True, 1.0):
            with self.assertRaises(ValueError):
                PressureWorkload(['does-not-exist'], value)


if __name__ == '__main__':
    unittest.main()
