# Pod OOM Risk

This alert means a recent snapshot contained OOM, OOM-kill, or hard-limit evidence. The alert uses a five-minute Prometheus lookback because the collector reports recent deltas rather than an ever-increasing application counter.

1. Run `kubectl memlens explain pod <pod> -n <namespace>` and note the confidence and evidence.
2. Inspect `kubectl describe pod/<pod> -n <namespace>` plus container restart reason and previous logs.
3. Compare `memory.peak`, current usage, configured limits, and the dominant memory bucket.
4. Capture the incident before short history expires: `kubectl memlens capture -n <namespace> --pod <pod> --include-history -o incident.json`.

Do not label this a memory leak without workload-specific growth evidence. A transient allocation, tmpfs growth, file-cache burst, or node-pressure episode can also cross the limit.
