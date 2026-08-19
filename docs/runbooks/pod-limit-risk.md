# Pod Limit Risk

This alert means current cgroup usage remained near `memory.max` for five minutes without stronger OOM or pressure evidence.

1. Run `kubectl memlens explain pod <pod> -n <namespace>` and compare current usage, peak, limit, and headroom.
2. Check bounded history for growth direction and event deltas.
3. Inspect every replica through `explain workload`; avoid sizing from one outlier without understanding why it differs.
4. Investigate the dominant bucket before changing requests or limits.

Treat this as early warning. It predicts low headroom, not a guaranteed OOM.
