# Architecture

KubeMemLens is designed as a terminal-first tool that can start locally and grow into a Kubernetes-aware inspector without changing the core memory model.

## Current Components

- `kubectl-memlens`: CLI and future kubectl plugin entrypoint.
- `internal/cgroup`: cgroup v2 file parser.
- `internal/model`: memory breakdown and formatting helpers.
- `internal/explain`: diagnosis heuristics and incident-friendly explanations.
- `examples/cgroup-v2`: local sample data for development and tests.
- `memlens-agent`: DaemonSet binary that scans node cgroups and posts snapshots.
- `memlens-collector`: in-memory HTTP collector for latest container snapshots.

## v0.2 Cluster Flow

1. The agent runs on each node as a DaemonSet.
2. The agent walks `/host/sys/fs/cgroup` and returns only container-looking cgroups.
3. The agent lists pods through the Kubernetes API and builds a pod/container index for its node.
4. The agent maps container cgroup IDs to namespace, pod, container, and node metadata.
5. The agent posts `AgentSnapshot` JSON to the collector at `/api/v1/snapshots`.
6. The collector stores the latest container snapshots in memory.
7. The CLI queries `/api/v1/pods` and `/api/v1/namespaces` through a user-managed port-forward.

Pod and namespace totals are sums of mapped container cgroups only. Parent pod cgroups are intentionally not added to avoid double-counting.

## Future Components

### CLI / kubectl plugin

The CLI remains the primary interface. Once installed as `kubectl-memlens`, users will be able to run it through kubectl as:

```sh
kubectl memlens
```

### Terminal dashboard

A Bubble Tea TUI is planned after the sample and Kubernetes data paths are stable. It should show pod/container memory buckets, diagnosis, and drill-down views without becoming a web dashboard.

### Node-local agent

The agent runs as a DaemonSet. It reads cgroup memory files from the node, maps cgroup paths to containers, and posts snapshots to the collector. A future version should replace per-interval pod listing with an informer/cache.

### Collector

The collector receives recent snapshots from node agents and makes them available to the CLI. Long-term storage is intentionally out of scope for now.

### Kubernetes metadata mapper

The Kubernetes package maps container IDs from pod status to cgroup paths. It currently uses simple list calls for v0.2 and keeps informer work as a later optimisation.

### Optional eBPF attribution

A future eBPF mode may attribute file-cache and allocation behaviour more precisely. That mode should be optional and reviewed separately because it changes the security model.
