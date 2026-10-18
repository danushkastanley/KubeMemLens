"""Shared clock and empty-state checks for bounded trace controllers."""
import json


def clock_ns(case):
    return json.loads(case.runtime.exec(['/usr/local/bin/kml-lifecycle-census', '--mode', 'clock']))


def empty(snapshot):
    return all(snapshot[k] == 0 for k in ('workers', 'excludedWorkers', 'activeControls')) and not any(snapshot['objects'].values())
