# Security Model

KubeMemLens is privacy-first and local-first.

## v0.2

The v0.2 tool reads local cgroup sample files, node cgroup v2 memory files, and Kubernetes pod/node metadata. It does not send telemetry and does not include SaaS behaviour.

## Host Access

DaemonSet mode uses hostPath mounts for `/sys/fs/cgroup` and `/proc`. Those paths are sensitive because they expose node-level process and container metadata. The chart mounts them read-only and keeps the root filesystem read-only by default.

## Permissions

v0.2 is intended to stay cgroup-read focused. It should not require privileged containers unless a specific environment requires different host access. The agent ServiceAccount needs `get`, `list`, and `watch` access for pods and nodes across namespaces so it can map cgroups to Kubernetes metadata.

## Telemetry

There is no telemetry by default. Any future export path should be explicit and local to the user's infrastructure unless the project intentionally adds a separate hosted product.

## Collector Storage

The collector stores latest snapshots in memory only. It has no database and no long-term retention in v0.2.

## Future eBPF Mode

An eBPF attribution mode may require elevated capabilities, kernel helpers, or privileged deployment settings. That mode needs a separate security review before implementation.
