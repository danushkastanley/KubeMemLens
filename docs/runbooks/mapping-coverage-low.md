# Mapping Coverage Low

This alert means an agent mapped fewer than 95 percent of discovered workload container cgroups to Kubernetes Pod metadata for five minutes.

1. Run `kubectl memlens doctor --strict` to confirm the live mapping ratio and runtime detection.
2. Check Pod list/watch RBAC and the node-filtered informer sync message in agent logs.
3. Compare the reported container runtime and cgroup driver with the compatibility matrix.
4. Preserve agent logs and a redacted incident bundle when opening an issue; never attach host cgroup paths or container IDs unless explicitly required.

Sandbox and runtime infrastructure cgroups are intentionally excluded, so investigate only the reported workload-container denominator.
