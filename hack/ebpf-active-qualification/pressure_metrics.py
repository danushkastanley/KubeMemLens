"""Continuous producer coverage and paired throughput, independent of trace totals."""
from pathlib import Path
import sys

sys.path.insert(0, str(Path(__file__).resolve().parents[2] / 'prototype/trace/qualification/filecache'))

from flood_resources import validate_containment_window
from samples import integer, require
from verify_pressure import validate_pressure

CLOCK_MARGIN_NANOS = 5000000


def boundaries(observation):
    header = observation['header']
    # At the initial instant the monotonic reading follows the wall reading.
    low = header['wallNanos'] - CLOCK_MARGIN_NANOS
    high = header['wallNanos'] + header['monotonicAfterNanos'] - header['monotonicBeforeNanos'] + CLOCK_MARGIN_NANOS
    result = []
    for row in observation['intervals']:
        end_high = row['wallNanos'] + CLOCK_MARGIN_NANOS
        end_low = row['wallNanos'] - (row['clockAfterMonotonicNanos'] - row['endedMonotonicNanos']) - CLOCK_MARGIN_NANOS
        result.append((low, high, end_low, end_high))
        low, high = end_low, end_high
    return result


def covered_intervals(observation, resources):
    spans = boundaries(observation)
    begin = resources[0]['wallNanos']
    end = resources[-1]['wallNanos'] + resources[-1]['readNanos']
    require(spans[0][1] <= begin and spans[-1][2] >= end,
            'continuous producer does not cover the complete resource window')
    # Exclude uncertain partial boundary intervals; never infer fractional bytes.
    inside_begin = begin + resources[0]['readNanos']
    inside_end = resources[-1]['wallNanos']
    indices = [index for index, (start_low, _, _, end_high) in enumerate(spans)
               if start_low >= inside_begin and end_high <= inside_end]
    return indices


def compare_pressure(control_raw, enabled_raw, control_resources, enabled_resources, *,
                     pressure_seconds, window_seconds, minimum_common_intervals):
    integer(minimum_common_intervals, 1, window_seconds)
    roles = {'agent', 'collector', 'selected'}
    validate_containment_window(control_resources, window_seconds, roles)
    validate_containment_window(enabled_resources, window_seconds, roles | {'node', 'api'})
    before, after = (validate_pressure(raw, pressure_seconds) for raw in (control_raw, enabled_raw))
    eligible_before = covered_intervals(before, control_resources)
    eligible_after = covered_intervals(after, enabled_resources)
    indices = sorted(set(eligible_before) & set(eligible_after))
    require(len(indices) >= minimum_common_intervals and indices == list(range(indices[0], indices[-1] + 1)),
            'insufficient contiguous paired pressure intervals')
    paired = []
    totals = {'controlBytes': 0, 'enabledBytes': 0, 'controlNanos': 0, 'enabledNanos': 0}
    for index in indices:
        a, b = before['intervals'][index], after['intervals'][index]
        a_time = a['endedMonotonicNanos'] - a['startedMonotonicNanos']
        b_time = b['endedMonotonicNanos'] - b['startedMonotonicNanos']
        totals['controlBytes'] += a['readBytes']
        totals['enabledBytes'] += b['readBytes']
        totals['controlNanos'] += a_time
        totals['enabledNanos'] += b_time
        paired.append({'producerSequence': index, 'controlReadBytes': a['readBytes'],
                       'enabledReadBytes': b['readBytes'], 'controlNanos': a_time, 'enabledNanos': b_time})
    numerator = totals['enabledBytes'] * totals['controlNanos']
    denominator = totals['controlBytes'] * totals['enabledNanos']
    require(denominator > 0, 'no measured pressure throughput')
    return {'scope': 'continuous producer coverage and matched interior throughput; no active-trace-only verdict',
            'continuousProducerCoversBothWindows': True, 'pairedIntervals': paired,
            'clockMarginNanos': CLOCK_MARGIN_NANOS,
            'commonIntervals': len(indices), 'totals': totals,
            'controlBoundaryIntervalsExcluded': len(before['intervals']) - len(indices),
            'enabledBoundaryIntervalsExcluded': len(after['intervals']) - len(indices),
            'throughputRegressionPercent': (1 - numerator / denominator) * 100,
            'selectedMatchedThroughputBudgetPassed': numerator * 100 > denominator * 98,
            'qualification': 'producer coverage and throughput only; attachment, stream, resource and lifecycle gates required'}
