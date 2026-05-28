# KubeMemLens

KubeMemLens is a terminal-first Kubernetes memory inspector.

It helps answer:

> Why is this pod's memory high?

Instead of showing one scary memory number, KubeMemLens separates pod/container memory into RSS/anonymous memory, file cache, tmpfs/shared memory, slab/kernel memory, dirty pages, writeback, and memory pressure signals.

The goal is to make incidents like "kubectl top says memory is high, but the app heap is stable" easier to understand.

## Status

KubeMemLens is at the v0.4 local-cluster stage. The sample CLI still works without Kubernetes, and the Helm chart can deploy a node-local agent plus in-memory collector for real cgroup snapshots in a local cluster. The CLI can query the collector through the Kubernetes API service proxy by default, with HTTP/port-forward mode available as a fallback.

## Example

```sh
go run ./cmd/kubectl-memlens sample explain cache-heavy
```

```text
Sample: cache-heavy

Total charged memory: 4.10 GiB
RSS / anon:           1.20 GiB
File cache:           2.60 GiB
Active file:          2.20 GiB
Inactive file:        380.00 MiB
Shmem / tmpfs:        90.00 MiB
Slab / kernel:        140.00 MiB
Dirty/writeback:      low

Diagnosis:
cache-heavy
```

```sh
go run ./cmd/kubectl-memlens sample top
```

## Development

```sh
go test ./...
go run ./cmd/kubectl-memlens sample list
go run ./cmd/kubectl-memlens sample explain cache-heavy
go run ./cmd/kubectl-memlens sample top
```

Make targets are available:

```sh
make test
make build
make run-sample-top
make run-sample-explain
make fmt
make vet
```

## Cluster Smoke Test

Start minikube with Docker:

```sh
minikube start --driver=docker --cpus=2 --memory=4096
```

Build and load a local image:

```sh
docker build -t kube-memlens:local-smoke .
minikube image load kube-memlens:local-smoke
```

Deploy the chart:

```sh
helm upgrade --install kube-memlens ./charts/kube-memlens \
  -n kube-memlens \
  --create-namespace \
  --set image.repository=kube-memlens \
  --set image.tag=local-smoke \
  --set image.pullPolicy=Never
```

When iterating quickly, use a fresh image tag or restart the workloads after `minikube image load` so the cluster does not keep an older local image.

Inspect the workloads:

```sh
kubectl get all -n kube-memlens
kubectl logs -n kube-memlens ds/kube-memlens-agent
kubectl logs -n kube-memlens deploy/kube-memlens-collector
```

Run a one-shot agent scan:

```sh
kubectl exec -n kube-memlens ds/kube-memlens-agent -- /memlens-agent --cgroup-root=/host/sys/fs/cgroup --once
```

Query the collector directly, if you want to inspect the HTTP API:

```sh
kubectl -n kube-memlens port-forward svc/kube-memlens-collector 18080:8080
curl http://127.0.0.1:18080/healthz
curl http://127.0.0.1:18080/api/v1/debug/store
curl http://127.0.0.1:18080/api/v1/pods
```

Use the CLI against collector snapshots without a manual port-forward:

```sh
go run ./cmd/kubectl-memlens status
go run ./cmd/kubectl-memlens top pods -A
go run ./cmd/kubectl-memlens top containers -A
go run ./cmd/kubectl-memlens top ns
go run ./cmd/kubectl-memlens explain pod <pod-name> -n <namespace>
```

## Using Without Port-Forward

By default, KubeMemLens uses the Kubernetes API service proxy to reach the in-cluster collector. The default collector target is:

- namespace: `kube-memlens`
- service: `kube-memlens-collector`
- port: `8080`

```sh
go run ./cmd/kubectl-memlens status
go run ./cmd/kubectl-memlens top pods -A
go run ./cmd/kubectl-memlens tui
```

Override the collector target when needed:

```sh
go run ./cmd/kubectl-memlens status \
  --collector-namespace=kube-memlens \
  --collector-service=kube-memlens-collector \
  --collector-port=8080
```

Use a specific kubeconfig or context:

```sh
go run ./cmd/kubectl-memlens top pods -A --kubeconfig=/path/to/config --context=minikube
```

## Fallback To Port-Forward

HTTP mode is still supported for local debugging or restricted RBAC environments:

```sh
kubectl -n kube-memlens port-forward svc/kube-memlens-collector 18080:8080
go run ./cmd/kubectl-memlens top pods -A --collector-url=http://127.0.0.1:18080
go run ./cmd/kubectl-memlens top containers -A --collector-url=http://127.0.0.1:18080
go run ./cmd/kubectl-memlens top ns --collector-url=http://127.0.0.1:18080
go run ./cmd/kubectl-memlens explain pod <pod-name> -n <namespace> --collector-url=http://127.0.0.1:18080
```

## Terminal Dashboard

Run the dashboard:

```sh
go run ./cmd/kubectl-memlens tui
```

Future installed usage:

```sh
kubectl memlens tui
```

Example layout:

```text
KubeMemLens | view: pods | connection: kube-proxy kube-memlens/kube-memlens-collector:8080 | refreshed: 2s ago

NAMESPACE     POD                         NODE       TOTAL   RSS    CACHE  SHMEM  SLAB   DIAGNOSIS
kube-system   coredns-...                 minikube   72Mi    31Mi   20Mi   0Mi    8Mi    normal
kube-memlens  kube-memlens-agent-...      minikube   40Mi    18Mi   12Mi   0Mi    5Mi    normal

q quit · r refresh · / search · tab switch · enter drill · e explain · ? help
```

## RBAC For Kube-Proxy Mode

The kubeconfig identity running the CLI needs permission to access the collector service proxy:

```sh
kubectl auth can-i get services/proxy -n kube-memlens
```

See `examples/rbac/kube-memlens-viewer.yaml` for a minimal Role that an admin can bind to users or groups.

## Current Scope

- cgroup v2 parser for `memory.current`, `memory.stat`, and `memory.events`
- cgroup walker for Kubernetes container cgroups
- memory model with clear buckets
- explanation engine for common high-memory profiles
- Cobra-based CLI skeleton
- sample data for local development
- node-local agent that maps cgroups to pod/container metadata
- agent to collector snapshot POST
- in-memory collector with pod and namespace aggregation
- collector-backed `top pods`, `top containers`, `top ns`, and `explain pod`
- Bubble Tea terminal dashboard with namespace, pod, container, and pod detail views
- Kubernetes API service proxy mode for CLI/TUI collector access
- `status` command for collector connectivity and latest snapshot counts

## Non-Goals

- No web dashboard
- No eBPF attribution yet
- No long-term storage
- No SaaS, telemetry, or exporters
- No auto-remediation
- No CRDs
- No automatic port-forwarding

## Security Posture

v0.4 reads cgroup files through a read-only `/sys/fs/cgroup` hostPath mount and reads pod/node metadata through the Kubernetes API. CLI kube-proxy mode uses the user's Kubernetes credentials and is governed by RBAC. KubeMemLens does not send telemetry, phone home, or persist workload data outside the in-memory collector. Future eBPF mode will need a separate security review.

## Roadmap

- v0.1: local parser and sample CLI
- v0.2: Kubernetes cgroup mapping, DaemonSet scan, collector snapshots, and collector-backed CLI
- v0.3: terminal dashboard, search/sort, pod explain view
- v0.4: kubectl-native collector connectivity through Kubernetes API service proxy
- v0.5: Prometheus/OpenMetrics export
- v0.6: agent informer cache and cgroup mapping hardening
- v0.7: optional eBPF file attribution

## Contributing

Small, focused issues and pull requests are welcome. Please keep language incident-friendly, avoid overclaiming root cause, and add tests when behaviour changes.
