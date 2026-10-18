import copy
import unittest

from observations import FILE_BYTES, complete, idle_under_noise, known_total, reference, selected_io
from transport import QualificationError


def total(value):
    return {'value': value, 'unreported': False, 'overflow': False}


def report(kind, count):
    return {'state': 'completed', 'transportComplete': True, 'validatedEventFrames': count if kind == 'files' else 0,
            'observed': {'kind': kind, 'paths': 'confirmed' if kind == 'files' else 'omit', 'streamVersion': 2},
            'summary': {'termination': 'expired', 'incomplete': True, 'rejectedEvents': 0,
                        'engineCounts': {'produced': count, 'sampled': 0, 'lost': 0, 'rejected': 0},
                        'aggregates': {'kind': kind, 'observations': count}}}


def workload(mode):
    return {'mode': mode, 'fileBytes': FILE_BYTES, 'pageBytes': 4096,
            'residentPagesBefore': 0 if mode == 'uncached' else 2048, 'residentPagesAfter': 2048,
            'readBytes': FILE_BYTES, 'writeBytes': 0}


class ObservationTests(unittest.TestCase):
    def test_idle_requires_measured_zero_even_when_hook_coverage_is_partial(self):
        self.assertEqual(idle_under_noise(report('files', 0), 'files')['produced'], 0)
        for value in [None, False, 1]:
            bad = report('files', 0); bad['summary']['engineCounts']['lost'] = value
            with self.assertRaises(QualificationError):
                idle_under_noise(bad, 'files')
        with self.assertRaises(QualificationError):
            idle_under_noise(report('files', 1), 'files')

    def test_missing_truncated_and_mismatched_streams_are_not_zero_evidence(self):
        for change in [lambda r: r.update(summary=None), lambda r: r.update(state='truncated'),
                       lambda r: r.update(transportComplete=False), lambda r: r['observed'].update(paths='omit')]:
            value = report('files', 0); change(value)
            with self.assertRaises(QualificationError):
                complete(value, 'files')

    def test_selected_file_bytes_match_the_independent_generator(self):
        value = report('files', 128)
        value['summary']['aggregates'].update(reads={'requestedBytes': total(FILE_BYTES), 'completedBytes': total(FILE_BYTES)},
                                              writes={'requestedBytes': total(0), 'completedBytes': total(0)})
        self.assertEqual(selected_io(value, 'files', workload('cached'))['readBytes'], FILE_BYTES)
        value['summary']['aggregates']['reads']['completedBytes'] = total(FILE_BYTES+65536)
        with self.assertRaises(QualificationError):
            selected_io(value, 'files', workload('cached'))

    def test_cache_uses_cold_workload_residency_not_warm_file_noise(self):
        value = report('cache', 4096)
        value['summary']['aggregates'].update(additions={'pages': total(2048)}, removals={'pages': total(2048)})
        self.assertEqual(selected_io(value, 'cache', workload('uncached'))['addedPages'], 2048)
        with self.assertRaises(QualificationError):
            selected_io(value, 'cache', workload('cached'))
        value['summary']['aggregates']['additions']['pages']['unreported'] = True
        with self.assertRaises(QualificationError):
            selected_io(value, 'cache', workload('uncached'))

    def test_workload_unknowns_and_corrupt_totals_fail(self):
        for key, value in [('readBytes', 0), ('residentPagesAfter', 0), ('pageBytes', 123), ('writeBytes', False)]:
            receipt = workload('cached'); receipt[key] = value
            with self.assertRaises(QualificationError):
                reference(receipt, 'cached')
        for value in [total(None), total(False), dict(total(1), overflow=True), dict(total(1), unreported=True)]:
            with self.assertRaises(QualificationError):
                known_total(value)


if __name__ == '__main__':
    unittest.main()
