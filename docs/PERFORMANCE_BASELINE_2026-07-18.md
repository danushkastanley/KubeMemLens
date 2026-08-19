# KubeMemLens Phase A Performance Baseline

Date: 18 July 2026
Host: Apple M4 Pro, arm64, macOS
Method: synthetic cgroup v2 fixture trees and synthetic one-container Pods; median of three runs after pressure/limit/swap parsing
Command:

```sh
go test ./internal/cgroup ./internal/kube \
  -run '^$' \
  -bench 'Benchmark(Walker|BuildPodIndexAndLookup)' \
  -benchmem \
  -count=3
```

The 5,000/10,000 fixture cases use `-benchtime=1x -count=3` so fixture creation remains bounded; the table reports the median of those three single iterations.

## Results

| Operation | Density | Time | Allocated bytes | Allocations |
|---|---:|---:|---:|---:|
| Walk and parse cgroups | 100 containers | 7.33 ms | 2,553,968 | 11,176 |
| Walk and parse cgroups | 1,000 containers | 85.11 ms | 25,823,717 | 111,125 |
| Walk and parse cgroups | 5,000 containers | 498.67 ms | 131,691,464 | 555,287 |
| Walk and parse cgroups | 10,000 containers | 1.087 s | 268,048,136 | 1,110,453 |
| Build Pod index and exact-look up every container | 100 Pods | 35.75 us | 79,120 | 222 |
| Build Pod index and exact-look up every container | 1,000 Pods | 376.88 us | 1,080,049 | 2,044 |
| Build Pod index and exact-look up every container | 5,000 Pods | 3.08 ms | 6,423,272 | 15,102 |
| Build Pod index and exact-look up every container | 10,000 Pods | 5.44 ms | 12,841,888 | 30,162 |

## Interpretation

- The synthetic cgroup walk is approximately linear in this range and dominates metadata indexing cost.
- Rebuilding an index from the local informer cache is sub-millisecond at 1,000 one-container Pods on this host.
- At a five-second interval, a synthetic 1,000-container filesystem walk occupied about 1.7% of one core for the measured scan duration. This is an inference from the benchmark, not a live-cluster CPU measurement.
- Optional peak, boundary, PSI, swap, and local-event reads add filesystem operations. Allocation volume in the walker is material and should be profiled before increasing default scan frequency.

## Linux kind-node fixture check

The end-to-end harness can cross-compile and run the 5,000/10,000 fixture cases inside its live kind node with `E2E_RUN_DENSITY_BENCHMARKS=true`. On 18 July 2026, a Kubernetes 1.34.8/containerd Linux arm64 kind node on the same Apple M4 Pro host produced these medians from three single iterations:

| Operation | Density | Time | Allocated bytes | Allocations |
|---|---:|---:|---:|---:|
| Walk and parse fixture directories | 5,000 | 139.70 ms | 117,434,928 | 545,509 |
| Walk and parse fixture directories | 10,000 | 295.28 ms | 238,349,400 | 1,090,551 |
| Build Pod index and exact-look up | 5,000 | 3.81 ms | 6,423,384 | 15,103 |
| Build Pod index and exact-look up | 10,000 | 6.87 ms | 12,841,888 | 30,162 |

The node container used its writable filesystem for synthetic cgroup-shaped directories. This verifies Linux binaries and filesystem behaviour in the live cluster node container, but it is not a live 10,000-container Kubernetes workload or cgroupfs capacity claim.

## Limitations and next measurements

This is a repeatable development baseline, not a production capacity claim. APFS, warm local files, synthetic shallow paths, one container per Pod, and a single host do not represent Linux cgroupfs or cluster churn. Before the developer preview, run the same workloads in the compatibility matrix and record:

- Linux cgroupfs timings for 100, 1,000, 5,000, and 10,000 containers.
- Agent CPU, memory, filesystem operations, and API watch traffic over at least 30 minutes.
- Containerd and CRI-O path layouts across declared Kubernetes versions.
- Cold-cache, warm-cache, churn, failed-watch/relist, and partial filesystem error cases.

The benchmark fixtures now include 5,000 and 10,000 cases for repeatable local and Linux-node runs. Those cases create synthetic directories and Pod objects; they are not evidence from a live 10,000-container cluster. A real density qualification must additionally measure Kubernetes watch/relist traffic, cgroupfs behaviour, collector response sizes, node contention, churn, and workload impact for the full soak window.

The collector API now also has a deterministic 10,000-record regression. The old full-array container response exceeded its default 16 MiB bound and correctly returned `507`; the bounded keyset-paged endpoint returned all 10,000 records in pages of at most 500, with automatic reduction if an encoded page reaches the byte ceiling. This proves the response contract under synthetic in-memory data, not live node or network performance. Use the [live density and churn runbook](qualification/live-density-soak.md) for the remaining 5,000/10,000 gate.

The harness itself subsequently passed a [20-container development smoke](qualification/local-live-density-smoke-2026-07-18.md), including a 30-second steady window and rolling-restart recovery. That result validates orchestration and evidence collection only; Metrics API data was unavailable and the density was intentionally below the release gate.
