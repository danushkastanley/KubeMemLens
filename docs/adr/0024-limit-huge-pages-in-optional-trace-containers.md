# ADR 0024: Limit huge pages in optional trace containers

Date: 1 October 2026

Status: accepted configuration change; resource qualification remains open

The later standard-chart extension and explicit trace parallelism are recorded
in [ADR 0025](0025-bind-runtime-settings-to-resource-profiles.md). The decisions
and failed measurements below retain their original scope.

## Context

The latest optional trace candidate failed its five-pair local idle qualification:
one pair reached 40.61 MiB combined working set against the unchanged 40 MiB gate.
The failure remains valid. A subsequent read-only diagnostic found 6–14 MiB of
anonymous huge pages per service on LinuxKit arm64, whose transparent huge-page
policy was `always` with 2 MiB pages. Mapping annotations did not identify the
allocation owner.

Two sequential 20-minute diagnostics used the same image and acceptance policy,
with fresh matched TLS prerequisites and process lifetimes. Setting only
`GODEBUG=disablethp=1` in the candidate containers reduced the sampled combined
peak from 38.7 MiB to 27.1 MiB. No anonymous huge pages were observed in that second
run. Its approximate combined average CPU was 3.07 millicores, with no recorded
throttling. The [numeric diagnostic records](../ebpf/evidence/thp-diagnostic-2026-10-01.json)
retain the observations and limitations.

These sparse, unpaired diagnostics support a candidate configuration change.
They do not supersede the failed qualification, prove a provider result, or
establish active-trace overhead.

## Decision

The separate optional chart sets `GODEBUG=disablethp=1` on its API and binding-node
containers. This uses the Go runtime's process-local control for heap mappings.
GC metadata can still use huge pages; the setting is not a guarantee that all
anonymous huge pages disappear. The separately executed incident worker uses
the fixed environment `GOTRACEBACK=none` and `GODEBUG=disablethp=1`; it does not
inherit caller-supplied runtime settings or any other environment values.

The worker setting extends this decision on 2 October. The failed normal run's
node subtree reached 45.738 MiB working set, including 16 MiB anonymous huge
pages. Inspection found that worker execution omitted the service heap setting.
Those subtree measurements do not identify every allocation's owner or establish
the worker change's savings. The launcher regression exercises an actual fixture
child with conflicting parent settings and requires the exact fixed environment.

The chart remains disabled by default. The standard chart, host kernel policy,
capabilities, seccomp, admission bounds and benchmark thresholds do not change.
No GC percentage or memory-limit tuning is introduced.

## Alternatives

Increasing the memory threshold would hide the retained failure. Changing the
host's huge-page policy would affect unrelated workloads. GC tuning and binary
splitting remain possible investigations, but are broader changes than this
measured process-local comparison warrants.

## Consequences and verification

Smaller pages may affect TLB efficiency and active throughput. The exact candidate
must therefore repeat the full five-pair idle protocol and remaining active and
provider cases. No supported profile or release-readiness claim follows from
this ADR. Go upgrades must recheck the setting's support and measured effect.

The chart contract pins the environment on both service types. Its render is
compared structurally with the configuration used in the completed diagnostic.
This proves configuration equivalence, not performance qualification.

## Migration and rollback

An optional-chart upgrade recreates the service Pods with the new environment;
use the existing idle/quiescent upgrade protocol. No stored-data migration is
needed. Roll back the chart to remove the setting, then verify matched service
identities and cleanup. The prior memory failure still applies to that candidate.
