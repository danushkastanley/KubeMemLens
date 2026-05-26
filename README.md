# KubeMemLens

KubeMemLens is a terminal-first Kubernetes memory inspector.

It helps answer:

> Why is this pod's memory high?

Instead of showing one scary memory number, KubeMemLens separates pod/container memory into RSS/anonymous memory, file cache, tmpfs/shared memory, slab/kernel memory, dirty pages, writeback, and memory pressure signals.

The goal is to make incidents like "kubectl top says memory is high, but the app heap is stable" easier to understand.

## Status

KubeMemLens is at the v0.2 local-cluster stage. The sample CLI still works without Kubernetes, and the Helm chart can deploy a node-local agent plus in-memory collector for real cgroup snapshots in a local cluster.

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

Query the collector:

```sh
kubectl -n kube-memlens port-forward svc/kube-memlens-collector 18080:8080
curl http://127.0.0.1:18080/healthz
curl http://127.0.0.1:18080/api/v1/debug/store
curl http://127.0.0.1:18080/api/v1/pods
```

Use the CLI against collector snapshots:

```sh
go run ./cmd/kubectl-memlens top pods -A --collector-url=http://127.0.0.1:18080
go run ./cmd/kubectl-memlens top ns --collector-url=http://127.0.0.1:18080
go run ./cmd/kubectl-memlens explain pod <pod-name> -n <namespace> --collector-url=http://127.0.0.1:18080
```

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
- collector-backed `top pods`, `top ns`, and `explain pod`

## Non-Goals

- No web dashboard
- No eBPF attribution yet
- No long-term storage
- No SaaS, telemetry, or exporters
- No auto-remediation
- No CRDs
- No automatic port-forwarding

## Security Posture

v0.2 reads cgroup files through a read-only `/sys/fs/cgroup` hostPath mount and reads pod/node metadata through the Kubernetes API. It does not send telemetry, phone home, or persist workload data outside the in-memory collector. Future eBPF mode will need a separate security review.

## Roadmap

- v0.1: local parser and sample CLI
- v0.2: Kubernetes cgroup mapping, DaemonSet scan, collector snapshots, and collector-backed CLI
- v0.3: terminal dashboard
- v0.4: Prometheus/OpenMetrics export
- v0.5: optional eBPF file attribution
- v0.6: on-demand pod tracing

## Contributing

Small, focused issues and pull requests are welcome. Please keep language incident-friendly, avoid overclaiming root cause, and add tests when behaviour changes.
