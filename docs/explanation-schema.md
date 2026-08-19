# Machine-readable Explanation Schema

`kubectl memlens explain pod ... -o json|yaml` and `explain workload ... -o json|yaml` emit explanation schema version `1`.

The contract contains:

- `schemaVersion`, generation time, and a namespaced target;
- explicit non-overlapping anonymous/file-cache/shmem/residual composition, overlapping kernel/slab/socket/page-table/mapped-file/THP detail, finite/unknown peak and limit state, PSI, recent event evidence, and scan/steal/refault/major-fault deltas;
- diagnosis, investigation severity, independent confidence and reason, summary, evidence, suggested checks, and explicit caveats;
- exact UTC observation bounds for point-in-time gauges and, when a prior container observation exists, exact counter-delta bounds plus complete/uniform flags and a distinct-window count;
- a restrained Kubernetes context for Pods, including requests/limits, QoS, runtime class, restart/termination state, node pressure/allocatable memory, owner, and bounded memory-backed `emptyDir` counts/limits;
- per-container or per-replica evidence without hiding outliers;
- copyable, read-only next commands.

The contract intentionally excludes Pod UID, container ID, cgroup path, arbitrary labels, image, file names, and raw Kubernetes objects. Consumers must reject unsupported `schemaVersion` values rather than guessing. New optional fields may be added within version 1; removals, meaning changes, or incompatible type changes require a new schema version.

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
