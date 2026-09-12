"""Bounded simultaneous Node measurements with separate counters and rotation."""

import math
import time
from concurrent.futures import ThreadPoolExecutor

from common import privacy, require, utc_text
from evidence import validate_rotation, validate_samples
from profiles import validate_profile
from sample import Window


class NodeWindows:
    def __init__(self, runtimes, phase, clock=time.monotonic):
        require(phase in {"baseline", "enabled"}, "invalid measurement phase")
        require(1 <= len(runtimes) <= 10, "invalid observation pool size")
        require(len({r.node for r in runtimes}) == len(runtimes), "duplicate observation Node")
        require(all(isinstance(r.node_uid, str) and r.node_uid for r in runtimes)
                and len({r.node_uid for r in runtimes}) == len(runtimes), "explicit unique Node identities are required")
        targets = {(tuple(r.kubectl), r.namespace, r.namespace_uid, r.workload_namespace, r.workload_uid)
                   for r in runtimes}
        require(len(targets) == 1, "observation Nodes must share the same bound target")
        self.phase, self.clock = phase, clock
        self.windows = [Window(r, phase, clock=clock) for r in sorted(runtimes, key=lambda r: r.node)]
        self.samples = [[] for _ in runtimes]
        self.started = None
        self.pool = ThreadPoolExecutor(max_workers=len(runtimes), thread_name_prefix="node-observation")

    def __enter__(self):
        self.started = self.clock()
        try:
            self.observe(retain=False)
        except BaseException:
            self.pool.shutdown(wait=True, cancel_futures=True)
            raise
        return self

    def __exit__(self, *_):
        self.pool.shutdown(wait=True, cancel_futures=True)

    def observe(self, retain=True):
        require(self.started is not None, "observation window has not started")
        futures = [self.pool.submit(window.observe, self.started) for window in self.windows]
        rows = [future.result() for future in futures]
        # Commit a complete pool sample only after every Node succeeded.
        if retain:
            for samples, row in zip(self.samples, rows):
                samples.append(row)
        return rows

    def result(self):
        nodes = []
        for slot, (window, samples) in enumerate(zip(self.windows, self.samples)):
            validate_samples(samples, self.phase)
            validate_rotation(window.rotation)
            nodes.append({"slot": slot, "samples": list(samples), "rotation": dict(window.rotation)})
        privacy(nodes)
        return nodes


def measure_nodes(runtimes, profile, phase, clock=time.monotonic, sleep=time.sleep):
    validate_profile(profile)
    require(phase in {"baseline", "enabled"}, "invalid measurement phase")
    require(len(runtimes) == profile["workload"]["linuxNodes"], "runtime pool differs from the profile")
    m = profile["measurement"]
    started_at = utc_text()
    sleep(m["settleSeconds"])
    with NodeWindows(runtimes, phase, clock) as windows:
        count = math.ceil(m[phase + "Seconds"] / m["sampleIntervalSeconds"])
        for index in range(1, count + 1):
            sleep(max(0, windows.started + index * m["sampleIntervalSeconds"] - clock()))
            windows.observe()
            print(f"{phase} pool measurement {index}/{count}", flush=True)
        result = {"schemaVersion": 2, "phase": phase, "startedAt": started_at, "completedAt": utc_text(),
                  "profile": {"id": profile["id"], "digest": profile["profileDigest"]},
                  "observation": {"method": "kubernetes-probes-v1", "image": profile["workload"]["image"]},
                  "nodes": windows.result()}
    privacy(result)
    return result
