# Machine-readable Explanation Schema

`kubectl memlens explain pod ... -o json|yaml` and `explain workload ... -o json|yaml` emit explanation schema version `3`. Version 2 introduced Pod resources; version 3 adds per-container MemoryQoS interpretation.

The contract contains:

- `schemaVersion`, generation time, and a namespaced target;
- explicit non-overlapping anonymous/file-cache/shmem/residual composition, overlapping kernel/slab/socket/page-table/mapped-file/THP detail, finite/unknown peak and limit state, PSI, recent event evidence, and scan/steal/refault/major-fault deltas;
- diagnosis, investigation severity, independent confidence and reason, summary, evidence, suggested checks, and explicit caveats;
- exact UTC observation bounds for point-in-time gauges and, when a prior container observation exists, exact counter-delta bounds plus complete/uniform flags and a distinct-window count;
- a restrained Kubernetes context for Pods, including requests/limits, QoS, runtime class, restart/termination state, node pressure/allocatable memory, owner, and bounded memory-backed `emptyDir` counts/limits;
- per-container or per-replica evidence without hiding outliers;
- copyable, read-only next commands.

The contract intentionally excludes Pod UID, container ID, cgroup path, arbitrary labels, image, file names, and raw Kubernetes objects. Consumers must reject unsupported `schemaVersion` values rather than guessing. Consumers should select the decoder by schema version. Older binaries retain their original explanation schemas. Snapshot negotiation is separate from explanation output, so existing live readers remain compatible.

`severity` is investigation urgency (`info`, `medium`, `high`, or `critical`); it is not confidence or a claim about business impact. Gauge values are instantaneous. `evidenceWindow.observationStart` and `observationEnd` are equal for a single collector snapshot and form explicit cross-snapshot bounds for a workload roll-up. Counter fields are intentionally separate:

- `counterDeltaKnown=false` means there is no elapsed counter window, normally on the first observation or after container identity changes;
- `counterDeltaComplete=false` means at least one included container had no prior observation;
- `counterDeltaUniform=false` means a roll-up spans multiple exact sampling windows; `counterDeltaStart` and `counterDeltaEnd` are the outer bounds, while per-container or per-replica findings retain their own window;
- `counterDeltaWindowCount` reports the number of distinct exact start/end pairs.

CLI and TUI text views surface the same timestamps and caveats. Incident capture preserves these collector-derived timestamps, so offline replay does not invent a new evidence window.

Example:

```sh
kubectl memlens explain pod api-abc -n production -o json > explanation.json
kubectl memlens explain workload deployment/api -n production -o yaml
```

`kubectl memlens recommend pod|workload` exports the same versioned target/finding contract plus composition-aware recommendations. The document always sets `automaticMutation: false`; it never emits an unreviewed resource patch.

## Resource context in version 2

`kubernetes.resources` reports configured Pod budgets, allocated request, applied
budgets, spec/observed generations and independent `pending`/`applying` resize
observations. Each resource value contains `bytes` and `known`; false means the
source did not report a value. Resize observations include a typed state, source,
and available generation/UTC transition time.

`kubernetes.effectiveResources` selects each configured request/limit with source
`pod-spec`, `container-sum`, `partial-container-sum`, `unset` or `unreported`.
The original `memoryRequestBytes` and `memoryLimitBytes` remain container totals.
Per-container `configuredResources` and `resources` retain container contributions
and kubelet observations. Workload replicas retain their own Pod resource context.
Cgroup evidence under `memory` keeps its existing meaning.

Snapshot API clients negotiate the same extension using
`X-KubeMemLens-Snapshot-Schema: 2`. Missing headers receive the original schema-1
representation. See [ADR 0005](adr/0005-negotiate-resource-snapshot-schemas.md).

Capture chooses incident schema 2 when resource metadata is present. Use
`kubectl memlens capture -n production --pod api-abc --schema-version=1 -o incident.json`
for an older replay binary; the export records that resource context was omitted.
Current replay accepts deep schemas 1 and 2 and [restricted incident schema 3](restricted-incidents.md). Unknown or mismatched schemas are rejected.

## MemoryQoS in version 3

Each container has `memoryQoS`; workload replicas carry named container
observations. `scope` is always `container-cgroup`. Protection (`memory.min`,
`memory.low`), throttle (`memory.high`) and hard limit (`memory.max`) each have
an explicit unavailable, zero, finite or unlimited state. These are controls,
not additional memory usage.

The observation preserves the high-event source and exact available delta
window, PSI, applied-or-configured container references and the separate Pod
configured limit. `state` distinguishes observed, unavailable, stale,
resize-unsettled and potentially-stale-or-inconsistent evidence. Confidence is
about this interpretation and is independent from the main composition finding.
A crossing with stalls is stronger evidence than a configured boundary alone.

An unlimited leaf `memory.high` does not prove that an ancestor is unlimited.
Parent controls, kubelet policy and kernel/runtime versions are not collected.
The contract carries those caveats instead of guessing feature-gate state or a
throttling factor. Recommendations retain schema 1 and remain read-only.
Existing incident schemas preserve the raw controls and replay recomputes the
interpretation; no capture migration is required.


## Restricted evidence

The development build emits explanation schema **4** and recommendation schema
**2** for restricted queries. Deep output remains explanation **3** and
recommendation **1**, including its existing fields and meanings.

Restricted documents contain `mode: restricted`, a namespaced `target`, a typed
`observation`, optional `children`, `sources`, `unavailable` query reasons,
`details`, read-only `recommendations` and `automaticMutation: false`. Working
set appears under `observation.workingSet`; its nullable `bytes`, API version,
sample/receive times, window, freshness and coverage retain their reader meaning.
Optional configured, Pod and container resource context is labelled separately.

Absent cgroup `memory` and `finding` fields are omitted. Consumers must reject
versions they do not support rather than supplying zero composition or a clean
diagnosis. These explanation versions do not change collector snapshots.
Restricted capture separately uses incident schema 3.
See [restricted workflows](restricted-mode.md) for commands, privacy and limits.
