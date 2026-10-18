"""Match validated trace totals to the independent fixed-seed workload receipts."""
from transport import QualificationError

FILE_BYTES = 8 * 1024 * 1024
REFERENCE_FIELDS = {'mode', 'fileBytes', 'pageBytes', 'residentPagesBefore', 'residentPagesAfter', 'readBytes', 'writeBytes'}


def reference(value, mode):
    operations = {'prepare': (0, 1), 'cached': (1, 0), 'uncached': (1, 0), 'write': (0, 1), 'noise': (4, 4)}
    if (mode not in operations or set(value) != REFERENCE_FIELDS or value['mode'] != mode or
            any(type(value[key]) is not int for key in REFERENCE_FIELDS-{'mode'}) or
            value['pageBytes'] not in (4096, 65536) or value['fileBytes'] != FILE_BYTES):
        raise QualificationError('independent workload receipt is invalid')
    pages = FILE_BYTES // value['pageBytes']
    reads, writes = operations[mode]
    before = 0 if mode in ('prepare', 'uncached') else pages
    if (value['residentPagesBefore'] != before or value['residentPagesAfter'] != pages or
            value['readBytes'] != reads*FILE_BYTES or value['writeBytes'] != writes*FILE_BYTES):
        raise QualificationError('independent workload contract failed')
    return value


def known_total(value):
    if (not isinstance(value, dict) or type(value.get('value')) is not int or value['value'] < 0 or
            value.get('overflow') is not False or value.get('unreported') is not False):
        raise QualificationError('trace total is unknown or invalid')
    return value['value']


def complete(report, kind):
    if kind not in ('files', 'cache'):
        raise QualificationError('unsupported isolation observation kind')
    summary = report.get('summary')
    if (report.get('state') != 'completed' or report.get('transportComplete') is not True or
            not isinstance(summary, dict) or summary.get('termination') != 'expired'):
        raise QualificationError('trace has no complete expiry evidence')
    counts, aggregates = summary.get('engineCounts'), summary.get('aggregates')
    if (not isinstance(counts, dict) or set(counts) != {'produced', 'sampled', 'lost', 'rejected'} or
            any(type(value) is not int or value < 0 for value in counts.values()) or
            any(counts[key] != 0 for key in ['sampled', 'lost', 'rejected']) or
            type(summary.get('rejectedEvents')) is not int or summary['rejectedEvents'] != 0 or
            type(summary.get('incomplete')) is not bool or not isinstance(aggregates, dict) or
            aggregates.get('kind') != kind or type(aggregates.get('observations')) is not int or
            aggregates['observations'] != counts['produced']):
        raise QualificationError('trace counts are incomplete or inconsistent')
    frames = report.get('validatedEventFrames')
    observed = report.get('observed', {})
    paths = 'confirmed' if kind == 'files' else 'omit'
    expected = counts['produced'] if kind == 'files' else 0
    if (type(frames) is not int or frames != expected or observed.get('kind') != kind or
            observed.get('paths') != paths or observed.get('streamVersion') != 2):
        raise QualificationError('trace disclosure policy or delivered count changed')
    return counts, aggregates


def idle_under_noise(report, kind):
    counts, aggregates = complete(report, kind)
    if counts['produced'] != 0 or aggregates['observations'] != 0:
        raise QualificationError('non-selected activity reached the trace')
    return {'produced': 0, 'delivered': 0, 'sampled': 0, 'lost': 0, 'rejected': 0}


def selected_io(report, kind, workload):
    mode = 'cached' if kind == 'files' else 'uncached'
    reference(workload, mode)
    counts, aggregates = complete(report, kind)
    if counts['produced'] <= 0:
        raise QualificationError('selected activity was not observed')
    if kind == 'files':
        reads, writes = aggregates['reads'], aggregates['writes']
        if (known_total(reads['requestedBytes']) != FILE_BYTES or known_total(reads['completedBytes']) != FILE_BYTES or
                known_total(writes['requestedBytes']) != 0 or known_total(writes['completedBytes']) != 0):
            raise QualificationError('selected file totals differ from independent I/O')
        return {'readBytes': FILE_BYTES, 'writeBytes': 0, 'produced': counts['produced'],
                'delivered': report['validatedEventFrames']}
    if kind == 'cache':
        pages = FILE_BYTES // workload['pageBytes']
        if (known_total(aggregates['additions']['pages']) != pages or
                known_total(aggregates['removals']['pages']) != pages):
            raise QualificationError('selected cache totals differ from independent residency')
        return {'addedPages': pages, 'removedPages': pages, 'produced': counts['produced'], 'delivered': 0}
    raise QualificationError('unsupported isolation observation kind')
