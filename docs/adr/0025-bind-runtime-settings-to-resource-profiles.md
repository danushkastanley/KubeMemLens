# ADR 0025: Bind runtime settings to resource profiles

Date: 2 October 2026

Status: implementation candidate; resource qualification remains open

## Context

The standard agent and collector did not use the heap huge-page control already
present in the optional trace services. A local comparison with the same standard
image and 32 synthetic fixture Pods reduced combined peak working set from
51.2 MiB to 39.3 MiB when only `GODEBUG=disablethp=1` changed. Combined observed
CPU increased by about 0.56 millicores. Each phase had 60 seconds of warmup and a
420-second resource window; 415 workload operations were fully covered in both
windows. This is one diagnostic pair, not a full qualification or provider result.

The optional Node service also exposes the host cgroup filesystem at
`/sys/fs/cgroup`. In its private cgroup namespace, the process reports membership
at `/` while that mount has a root above the namespace. Go cannot resolve the
container's CPU quota through this view. The container has a two-CPU limit, but
its mounted root reports an unlimited quota.

Three capability-free, one-shot Pods used the same accepted worker executable
and exited before request handling or BPF loading. With a two-CPU limit, the
ordinary cgroup view selected Go parallelism of two; the host-mounted view
selected 14, matching the local node's visible CPUs. Explicit `GOMAXPROCS=2`
restored two under the host-mounted view. The API service has an ordinary cgroup
view and already discovers its quota correctly.

## Decision

The standard chart sets `GODEBUG=disablethp=1` on agent and collector. This limits
heap huge-page use without changing host policy, capabilities or resource limits.
The setting remains a Go compatibility control and must be reviewed on toolchain
upgrades; see the [Go runtime guidance](https://go.dev/doc/gc-guide#Linux_transparent_huge_pages).

The optional Node container sets `GOMAXPROCS=2`, matching its fixed two-CPU limit.
Its worker launcher supplies exactly `GOTRACEBACK=none`, `GODEBUG=disablethp=1`
and `GOMAXPROCS=2`. It continues to reject inherited runtime, proxy and credential
environment values. The API retains automatic quota discovery. Go parallelism
does not cap operating-system thread counts or replace the container CPU quota.

## Alternatives

Changing host huge-page policy affects unrelated workloads. Process-wide huge-page
disable showed no consistent benefit in a short non-loading loader comparison.
Moving the host cgroup mount would change the reviewed target-resolution boundary.
Relying on automatic quota discovery retains the reproduced parallelism mismatch.
Increasing resource acceptance thresholds would conceal the existing failures.

## Consequences and verification

Smaller heap pages can trade CPU time for memory. Fewer Go execution slots may
affect startup or high-rate throughput, so the combined candidate must repeat the
unchanged idle, active and provider protocols. Existing CPU/memory failures remain
failures. No resource-qualified profile or release-readiness claim follows here.

The chart contracts require the heap setting and match trace parallelism to the
two-CPU limit. The real launcher fixture checks both the exact environment and
effective Go parallelism while its parent supplies conflicting values. Image
verification, sealed execution, admission, output and cleanup limits remain intact.

## Migration and rollback

Chart upgrades recreate the affected service Pods. Drain optional sessions using
the existing quiescent upgrade protocol first. No data migration is required.
Rollback restores the previous chart and matched executable/policy bundle, then
verifies cleanup. Review the fixed parallelism whenever the trace CPU limit changes.
