"""Measure a complete fixed window; never replace missing metrics with zero."""

import argparse
import math
import sys
import time
from pathlib import Path

from common import ContractError, load, require, utc_text, write_new
from kind_runtime import KindRuntime
from profiles import validate_profile


class Window:
    def __init__(self, runtime, phase, clock=time.monotonic):
        self.runtime, self.phase, self.clock = runtime, phase, clock
        self.initial_id = None
        self.initial_identity = None
        self.rotation_reads = None
        self.rotation = {"observed": False, "sameProducer": False,
                         "continuedAcquisition": False, "elapsedSeconds": None}

    def observe(self, started):
        r, enabled = self.runtime, self.phase == "enabled"
        now = self.clock()
        containers = r.containers()
        require(("node-context" in containers) == enabled, "producer state differs from phase")
        kubelet_cpu, kubelet_memory = r.kubelet(now)
        agent = r.component_metrics(containers["agent"], 8082)
        workload, mapped = r.workload()
        status = r.api("/clusterstatus/current")["store"]
        # Baseline freshness refers to the cgroup source; enabled needs both sources.
        fresh = status["reliability"]["freshNodes"]
        if enabled:
            fresh = min(fresh, status["nodeContext"]["freshRecords"])
        restarts, oom = r.stability()
        s = {"elapsedSeconds": now - started, "producerCPUMilli": None,
             "producerMemoryBytes": None, "kubeletCPUMilli": kubelet_cpu,
             "kubeletMemoryBytes": kubelet_memory,
             "agentScanSeconds": agent["kubememlens_agent_last_scan_duration_seconds"],
             "sourceReads": None, "sourceFailures": None, "lastReadSeconds": None,
             "lastResponseBytes": None, "workloadContainers": workload,
             "mappedContainers": mapped, "freshNodes": fresh,
             "producerReplicas": int(enabled), "unexpectedRestarts": restarts,
             "unexpectedOOMKills": oom}
        if enabled:
            self.producer(containers["node-context"], s, now)
        return s

    def producer(self, container, sample, now):
        r = self.runtime
        cpu, memory = r.producer_resources(container, now)
        values = r.component_metrics(container, 8083)
        prefix = "kubememlens_node_context_"
        reads = int(values[prefix + 'reads_total{result="success"}'])
        failures = sum(v for k, v in values.items() if k.startswith(prefix + "reads_total{")
                       and k != prefix + 'reads_total{result="success"}')
        sample.update(producerCPUMilli=cpu, producerMemoryBytes=memory,
                      sourceReads=reads, sourceFailures=int(failures),
                      lastReadSeconds=values[prefix + "last_read_seconds"],
                      lastResponseBytes=int(values[prefix + "last_response_bytes"]))
        identity = r.projected_identity(container)
        if self.initial_id is None:
            self.initial_id, self.initial_identity = container["id"], identity
        require(self.initial_id == container["id"], "producer replaced during rotation window")
        if identity != self.initial_identity and self.rotation_reads is None:
            self.rotation_reads = reads
            self.rotation.update(observed=True, sameProducer=True,
                                 elapsedSeconds=sample["elapsedSeconds"])
        if self.rotation_reads is not None and reads > self.rotation_reads:
            self.rotation["continuedAcquisition"] = True


def measure(runtime, profile, phase, output):
    m = profile["measurement"]
    time.sleep(m["settleSeconds"])
    started = time.monotonic()
    window = Window(runtime, phase)
    window.observe(started)  # Prime counters; exclude this instantaneous observation.
    samples = []
    interval = m["sampleIntervalSeconds"]
    count = math.ceil(m[phase + "Seconds"] / interval)
    for index in range(1, count + 1):
        time.sleep(max(0, started + index * interval - time.monotonic()))
        samples.append(window.observe(started))
        # Progress contains no workload or cluster identity.
        print(f"{phase} measurement {index}/{count}", flush=True)
    write_new(output, {"profile": {"id": profile["id"], "digest": profile["profileDigest"]},
                       "samples": samples, "rotation": window.rotation, "completedAt": utc_text()})


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ("cluster", "node", "kubeconfig", "profile", "output"):
        parser.add_argument("--" + name, required=True)
    parser.add_argument("--phase", choices=("baseline", "enabled"), required=True)
    args = parser.parse_args()
    try:
        profile = validate_profile(load(args.profile))
        require(profile["profileClass"] == "local-kind", "kind observer requires a local profile")
        require(not Path(args.output).exists(), "measurement output already exists")
        runtime = KindRuntime(args.cluster, args.node, args.kubeconfig)
        measure(runtime, profile, args.phase, args.output)
        return 0
    except ContractError as error:
        print(f"local measurement failed: {error}", file=sys.stderr)
        return 1
    except (OSError, ValueError, KeyError, TypeError) as error:
        print(f"local measurement failed ({type(error).__name__}); no qualification granted", file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
