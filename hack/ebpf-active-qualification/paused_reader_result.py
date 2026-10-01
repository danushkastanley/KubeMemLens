"""Retain a deliberate read pause without treating it as saturation evidence."""
import json

from ceiling_result import evaluate_ceiling
from samples import exact, integer, require, strict_json


def evaluate_paused_reader_session(raw, **bounds):
    require(type(raw) is str and len(raw.encode()) <= 65536 and raw.endswith('\n'),
            'incomplete or unbounded paused-reader output')
    lines = raw.splitlines()
    require(len(lines) == 2, 'one readiness record and one paused-reader result required')
    ready, document = (strict_json(line) for line in lines)
    exact(ready, {'schemaVersion', 'case', 'ready'})
    integer(ready['schemaVersion'], 1, 1)
    require(ready['case'] == 'paused-reader-ready' and ready['ready'] is True,
            'paused-reader readiness unconfirmed')
    exact(document, {'schemaVersion', 'case', 'sameKernelClock', 'pause', 'observation'})
    require(document['case'] == 'paused-reader-observation', 'wrong paused-reader case')
    pause = document['pause']
    exact(pause, {'requestedNanos', 'startedUnixNano', 'endedUnixNano', 'elapsedNanos', 'completed'})
    integer(pause['requestedNanos'], 5000000000, 5000000000)
    integer(pause['startedUnixNano'], 1, 2**63 - 1)
    integer(pause['endedUnixNano'], pause['startedUnixNano'], 2**63 - 1)
    integer(pause['elapsedNanos'], 5000000000, 40000000000)
    require(pause['completed'] is True, 'application read pause incomplete')
    require(abs(pause['endedUnixNano'] - pause['startedUnixNano'] - pause['elapsedNanos']) <= 5000000,
            'pause wall and monotonic clocks disagree')
    ceiling = {key: document[key] for key in ('schemaVersion', 'sameKernelClock', 'observation')}
    ceiling['case'] = 'ceiling-observation'
    result = evaluate_ceiling(json.dumps(ceiling), **bounds)
    result.update(scope='reported-admitted-ceiling-with-application-read-pause',
                  pause=dict(pause), readyBeforeResult=True,
                  backpressureEvidence='not established by an application read pause')
    return result
