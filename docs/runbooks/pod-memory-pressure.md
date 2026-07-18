# Pod Memory Pressure

This alert means KubeMemLens observed sustained cgroup reclaim throttling or memory PSI. It is evidence of impact, not proof that the application needs a larger limit.

1. Run `kubectl memlens explain pod <pod> -n <namespace>` and inspect PSI, `memory.high` deltas, swap, and the dominant bucket.
2. Run `kubectl memlens history pod <pod> -n <namespace>` and align the pressure window with latency, throughput, deploys, and restarts.
3. Run `kubectl memlens explain workload <kind>/<name> -n <namespace>` to check whether one replica is an outlier.
4. Inspect Node `MemoryPressure`, eviction events, and neighbouring workloads.

Do not increase the limit from total memory alone. High file cache without refault/PSI impact is different from sustained anonymous growth. Escalate when PSI or high-event deltas persist with user-visible degradation.
