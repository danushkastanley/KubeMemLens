# Volume context contract

The volume context model keeps filesystem usage, memory-backed configuration
and CSI health separate. It does not add volume bytes to cgroup memory charge.
The optional collector API provides current Pod volume configuration and kubelet
filesystem measurements, with separately enabled CSI health. Collection defaults
to off. The CLI and TUI use fresh caller-authorised reads for volume views,
recommendations, comparisons and volume incident capture.

## Identity and access

The join interface receives an already authorised Pod scope and current volume
bindings. The Kubernetes adapter must resolve the namespace, Pod UID, creation
time, scheduled Node name and Node UID. A PVC binding additionally requires its
current UID and creation time, PV claim binding, and generic ephemeral owner
verification where applicable. Summary PVC references contain no UID and cannot
establish these relationships independently.

Joins reject cross-namespace data, ambiguous duplicate volumes, a different Pod
or Node lifetime, and samples older than the current Pod or PVC. CSI health
reports carry the Node UID resolved during acquisition. Adapters must invalidate
cached identities when those objects change.

The integration access policy is:

| Evidence | Required disclosure boundary |
| --- | --- |
| Pod volume configuration | Authenticated volume-query entitlement and current Pod access |
| PVC identity and filesystem usage | Above, plus access to the referenced PVC and verified current binding |
| PVC controller health | Above, plus current PVC access |
| PV-derived driver identity | Current access to the bound PV and verified claim reference |
| CSINode backend health | Current CSINode access and the matching authorised driver binding |
| Workload volume context | Only independently authorised constituent Pods and volume bindings |

Node-context read access alone must not disclose tenant volumes. Collector-side
acquisition credentials do not grant callers access. The read handler must
delegate checks using the original caller's identity, groups and extras. A warm
cache cannot substitute for authorisation. Pod access loss denies the query;
loss of an optional source permission removes that source's protected detail.
An unavailable claim binding retains the authorised Pod's volume configuration
with a forbidden, unavailable or unreported usage state. It cannot retain a PVC
name, UID, creation time, driver or filesystem sample.

The optional `pods/volumes` endpoint and its viewer role are separate from the
existing memory and Node viewer roles. No viewer is bound automatically.

## Evidence and uncertainty

Configuration records the volume kind, memory-backed emptyDir setting,
configured size limit and mount/read-only counts. It contains no mount path or
container name and does not attribute memory charge to a particular volume.

Filesystem fields contain capacity, used and available bytes, inode total, used
and free counts, and the original source timestamp. Missing fields are omitted;
a pointer to zero means measured zero. Reserved space can make used plus
available differ from capacity. Missing measurements are not zero or healthy.
Filesystem completeness is partial until all six byte and inode fields are
reported, including measured zeros. It remains separate from source freshness.

Pod, PVC controller and CSINode backend health retain separate states and
timestamps. Backend health describes a Node/driver, not a measured fault in
each attached volume. Unknown future conditions remain visible and adverse.
A current unavailable or unreported health source can retain a separate
`health.lastGood` report for up to two minutes. That historical report is explicitly
stale, retains adverse/unknown flags and its original observation time, and is
removed from protected output after permission loss. A health transition is not a probe
heartbeat, and a recent API read does not establish recent driver probing.

Availability distinguishes reported, unreported, disabled, driver-unsupported,
forbidden, unavailable and unknown. Usage freshness is independent of health.
An available Summary response with no record for a volume produces unreported
usage. Callers must not infer a disabled gate or unsupported driver from absence.
Adapters supply source-specific health availability when no conditions exist.

## Bounds and retention

| Limit | Ceiling |
| --- | --- |
| Volumes per Pod | 64 |
| Mounts per volume | 64 |
| Health conditions per source | 16 |
| Name / UID | Kubernetes naming rules, at most 253 / 128 bytes; volume name at most 63 bytes |
| Condition status / reason | 256 bytes each; no control or directional format characters |
| Transient health message | 1,024 bytes; removed before retaining a joined report |
| Producer volume batch | 1,024 records and 1 MiB encoded |
| Named Pod result | 256 KiB encoded, including all health and configuration fields |
| Future paged query | At most 100 records and 256 KiB per page |
| Collector volume retention | 64 MiB total including last-good records and indexes; health reserves 16 MiB, leaving 48 MiB for usage when enabled |
| Health cache | 1,024 entries, 16 MiB, 32 KiB per sanitised status payload |
| Usage freshness / expiry | Stale after 45 seconds; omitted after two minutes |

Encoded-byte limits apply in addition to field counts. A maximum count of
maximum-sized fields is not guaranteed to fit. Inputs exceeding either ceiling
are rejected explicitly. The private decoder rejects unknown fields, duplicate
keys, case aliases, oversized collections, excessive nesting, invalid integers
and trailing JSON before admitting records. Error text contains no identities.

The existing Node observation stays limited to 16 KiB. Volume batches are
separate from Node memory. The opt-in parser shares the existing single Summary request, 4 MiB response
ceiling, five-second deadline and 15-second producer interval. It bounds both
scanned Pod/volume entries and retained records; omitted measurements still count
towards input ceilings. Malformed volume data produces an independent source
failure while a valid Node memory observation remains usable.

The collector retains current and last-good volume batches separately from Node
latest/history data. Last-good measurements keep their original source time and
appear only as `usage.lastGood`, labelled stale, after omission or source failure.
Denied, disabled and explicitly unsupported sources clear them. Binding changes
exclude earlier Pod/PVC samples, and expired values disappear after two minutes.
Retention limits include encoded current/last-good data and conservative index
charges. They are ceilings, not a measured scale qualification.

Current binding resolution has a five-second total deadline, 1 MiB per upstream
object and 8 MiB total read budget. Typed Pod/PVC/PV collections are bounded before
allocation. The named response and client decoder both enforce 256 KiB.

## Output and compatibility

`Report.Authorised()` explicitly produces names and sanitised condition reasons
for the authorised interactive response. It returns a copy. Default report JSON
uses `Redacted()`, retaining numeric/configuration evidence and fixed health
states without names, UIDs, driver identifiers, statuses or free-form reasons.
Record order is not a cross-capture identity. Metrics use fixed aggregate fields
from `Summary()`. Formatted reports and private batches contain no identities.

Only `Batch.EncodePrivate()` emits producer identity for authenticated transport
and bounded internal storage. Ordinary batch JSON contains a count and source
availability. Do not use the private encoder for diagnostic files, logs, metrics
or default incident captures. Free-form backend messages are never retained by
the join, even in the named representation.

The volume wire contract is version 1. Snapshot schema 4 carries private volume
batches and enables the named Pod-volume resource. Snapshot schema 5 adds the
optional historical health report. Schema 4 readers receive current health with
`health.lastGood` projected out; usage ingestion remains compatible. Schemas 1/2/3
omit volume batches and keep their existing representations; the new route is
hidden from readers that negotiate those older schemas. Snapshot schema 6 adds container `ioPressure`, immutable binding references in
authorised volume responses and the workload-volume route. Schemas 1 through 5
project out I/O pressure; schemas 4/5 omit binding references. Incident schema 5
is active for one Pod with volume evidence. Deep incident schemas 1/2, restricted 3
and Node 4 keep their meanings. Integration must explicitly project unsupported
fields for old readers and report omitted evidence in legacy captures.

No durable database migration is required. Disable `volumeContext.health` to
stop health acquisition while preserving filesystem and memory evidence. Disable
`nodeContext.volumeStats`
first, then `volumeContext.enabled`, before downgrading the collector. Node and
cgroup memory continue when only volume collection is disabled.

## Enable the scoped API

Start with the verified [Node-context profile](node-context.md), including its
explicit kubelet CA, token audience and network settings. Add these values to the
same Helm values file; the listed namespaces must already exist:

```yaml
nodeContext:
  enabled: true
  volumeStats: true
volumeContext:
  enabled: true
  namespaces: [team-a]
```

Upgrade the collector before enabling the producer flag when managing components
separately. The chart creates Pod/PVC `get` acquisition roles only in the listed
namespaces, plus a private collector PV `get` role for claim-reference validation.
It adds no Kubernetes mutation rights, CSI socket access or cloud credentials.
Volume collection reuses the existing Node-context producer identity and its
`nodes/stats` permission.

Bind the optional viewer role only in the namespace the viewer should inspect:

```sh
kubectl create rolebinding memlens-volume-viewer --namespace team-a \
  --clusterrole kube-memlens-volume-viewer --serviceaccount team-a:viewer
```

Using that viewer's kubeconfig, run `kubectl proxy --address=127.0.0.1 --port=8001`
in one terminal. In another, explicitly negotiate schema 6:

```sh
curl --fail --silent --show-error \
  --header "X-KubeMemLens-Snapshot-Schema: 6" \
  http://127.0.0.1:8001/apis/memory.kubememlens.io/v1alpha1/namespaces/team-a/pods/app/volumes
```

Stop the local proxy after the read. This viewer role grants `get` on the
aggregated `pods/volumes` resource and the underlying Pods/PVCs. It grants no
Node, CSINode or PV access. PV-derived driver names are omitted unless separately
authorised. The response metadata carries the authorised Pod UID so asynchronous
clients can reject a replaced selection; default redacted captures omit UIDs.

For direct process configuration, the collector uses
`--volume-context-namespaces=team-a` and `--volume-stats-enabled`; the separate
Node-context producer uses `--volume-stats`. Empty namespace configuration leaves
the route disabled. Stats ingestion requires the Node producer role. A profile
with the route enabled but stats disabled reports configuration with disabled
usage. `--volume-health-enabled` adds the independently controlled health adapter.

Operational counters use no PVC or Pod labels:
`kubememlens_volume_stats_reads_total`,
`kubememlens_volume_stats_errors_total`,
`kubememlens_volume_stats_records_total` and
`kubememlens_volume_stats_omissions_total`.

## Local verification

The disposable kind check extends Node-context authentication and isolation tests
with the pinned upstream CSI hostpath driver, a volume created through its local
CSI controller API, an isolated 128 MiB tmpfs test backend, controlled writes,
byte/inode deltas, original-caller revocation,
Pod replacement, expiry and independent volume rollback:

```sh
NODE_CONTEXT_ACKNOWLEDGE=create-and-remove-node-context-kind \
NODE_CONTEXT_VERIFY_INGESTION=true NODE_CONTEXT_VERIFY_VOLUME_STATS=true \
NODE_CONTEXT_CLUSTER=kube-memlens-node-context-volumes \
NODE_CONTEXT_ARTIFACT_DIR=/absolute/path/to/new-evidence \
  hack/verify-node-context-kind.sh
```

The fixture accelerates kubelet filesystem aggregation to five seconds; production
collection cadence remains unchanged. It removes its owned cluster, images and
private credentials. Only sanitised receipts leave the temporary work directory.
This proves the local path and does not qualify managed CSI providers, network
policy enforcement, storage latency or memory attribution.

## Optional CSI health

Set `volumeContext.health: true` alongside the enabled volume profile. Pod and
PVC health use the same live, authorised binding reads as filesystem context.
Backend health uses target-specific CSINode reads. The profile adds only collector
`get` access to CSINodes; it does not enable the Kubernetes alpha feature gate,
install a controller health sidecar or add status-write permissions.

The existing namespace volume viewer can inspect Pod and PVC-controller health.
Backend health also requires current caller access to the bound PV and CSINode.
The optional `kube-memlens-volume-backend-viewer` ClusterRole grants those
cluster-scoped reads and remains unbound. Use a custom ClusterRole with
`resourceNames` when access should cover specific PVs and Nodes.

```sh
kubectl create clusterrolebinding memlens-volume-backend-viewer \
  --clusterrole kube-memlens-volume-backend-viewer \
  --serviceaccount team-a:viewer
```

Authorisation is checked on every query, including cache hits. Revoking PVC access
removes protected controller/backend detail while authorised Pod health remains.
Revoking PV or CSINode access removes backend detail independently. A cache never
stores caller permission decisions. No driver messages enter retained payloads;
named output may contain bounded condition reasons from an authorised source.

The collector limits health-enabled queries to four concurrent operations and
20 combined acquisition/authorisation operations per second, with burst 40.
Core object reads retain the five-second query and 8 MiB byte budgets. Backend
reads have a two-second timeout, four concurrent requests and a global five-read
per-second limit with burst 10. Each Node/driver refreshes no more often than
15 seconds; source failures back off to 30 then 60 seconds. A short caller budget
cannot turn into a shared backend failure. Source failure and caller denial stay
separate, and caller cancellation cannot poison another reader's cached health.

Status payloads are immutable and deduplicated independently of observation time.
Unchanged health does not rewrite condition payloads or advance transition times.
Idle entries and expired last-good payloads are pruned every 15 seconds; expired
values stop appearing in responses after two minutes. New object lifetimes cannot
join old entries. Cache cleanup stops with the server and rejects late writes.

Targeted reads preserve the profile's existing `get`-only namespace permissions.
They also reuse the fresh Pod/PVC/PV reads needed to validate live bindings;
namespace-wide list/watch permissions would not replace those checks. Consumers
should use a separate 15-second volume refresh cadence rather than attaching a
multi-object volume read to every memory refresh.

For local gate-off verification, add `NODE_CONTEXT_VOLUME_HEALTH_PROFILE=off` to
the volume fixture command. Use `alpha` with Kubernetes 1.37 to test driver-originated
Pod/backend health, separately labelled API-seeded PVC-controller conditions,
conflicts, recovery, historical health projection and individual source revocation.
The fixture's direct CSI socket access and status patches belong only to the
owned test resources. Product acquisition remains read-only through Kubernetes.

## Operator workflow

The volume commands require the authenticated Kubernetes API connection. Bind
both the namespace memory viewer and the volume viewer to inspect memory and
volumes together. The volume-only viewer remains useful for the scoped raw API.

```sh
kubectl create rolebinding memlens-memory-viewer --namespace team-a \
  --clusterrole kube-memlens-namespace-viewer --serviceaccount team-a:viewer
kubectl memlens volumes pod app -n team-a
kubectl memlens explain pod app -n team-a --volumes
kubectl memlens recommend pod app -n team-a --volumes
```

Interactive text, JSON and YAML contain authorised names. Volume explanations use
output schema 5; volume-aware recommendations use schema 3. Filesystem byte/inode
saturation, memory-backed configuration and each health source have separate
labels. Recommendations provide read-only checks and never resize or repair
storage. Dirty/writeback, cache movement and I/O stalls are supporting evidence;
correlation cannot establish a storage cause or attribute charge to a volume.

In the TUI, select a Pod or workload and press `v`. Press `e` for memory details,
`g`/`G` to reach the first/last section, and Space to pause. Volume reads refresh
at most every 15 seconds independently of memory polling. Stale values retain
source times; values expire after two minutes even while paused. Confirmed
permission loss clears protected volume data and pending actions.

Press `R` for fresh recommendations. In a Pod volume view, `C` opens a redacted
capture and `x` marks a fresh comparison source; select a Pod and press `x` again
to compare. The second action rechecks access to the first Pod. Comparison marks
expire after two minutes. `y` explicitly copies the follow-up command for the
selected scope; it does not copy the evidence payload.

## I/O pressure

The cgroup reader optionally reads `io.pressure`, bounded to 4 KiB. A missing,
unreadable or invalid file produces an explicit enrichment state and preserves
valid memory evidence. I/O percentages and counters are per container. The view
reports coverage, sample times and the highest observed container averages;
it never sums PSI into a Pod, workload or memory total. Counter deltas require
the same Pod and container instance; resets leave the delta window unknown.
I/O PSI is neither storage-operation latency nor proof of a fault in a named
volume. No block-device or per-volume latency collection is included.

## Workload scope

Set `volumeContext.workloads: true` in addition to the volume profile. It creates
separate namespace acquisition roles with Pod/Job list and controller get
permissions, and an unbound `kube-memlens-workload-volume-viewer` ClusterRole.
Existing viewer roles are unchanged. Bind it only in the intended namespace:

```sh
kubectl create rolebinding memlens-workload-volumes --namespace team-a \
  --clusterrole kube-memlens-workload-volume-viewer --serviceaccount team-a:viewer
kubectl memlens volumes workload Deployment/app -n team-a
kubectl memlens explain workload Deployment/app -n team-a --volumes
kubectl memlens recommend workload Deployment/app -n team-a --volumes
```

Deployment, ReplicaSet, StatefulSet, DaemonSet, ReplicationController, Job and
CronJob queries resolve live controller UIDs and independently authorised Pod
bindings. Selected labels alone do not establish ownership. Parent identities
and access are checked again before publication. Unscheduled Pods and missing
cgroup observations remain explicit partial coverage.

The endpoint `workloads/{name}/volumes?kind=Deployment` requires snapshot schema
6. Queries are bounded to 32 Pods, 100 volume references, 1,024 containers and
256 KiB. CronJob inventory permits at most 256 Jobs and eight continuation pages.
Metadata resolution has an eight-second deadline; response composition is
limited to nine seconds. Binding metadata and its access checks share a 20-operation/second, burst-40
limiter. Composition additionally checks memory disclosure once per selected Pod
(at most 32 checks), within the same nine-second response deadline. The HTTP handler allows one active volume
query and returns 429 for excess queries; ordinary memory reads use a separate
gate. A large workload that exceeds these bounds must be inspected by Pod.

Shared PVC filesystems are deduplicated using current namespace/PVC UID/PV UID
bindings. Each Pod mount remains visible. Different mount observations retain a
caveat; filesystem numbers are never summed. Binding references are neither
access tokens nor proof against remounting or reformatting. Default redacted
exports remove both `evidenceID` and `filesystemID`.

## Volume incidents and comparison

Capture one named Pod with fresh access checks, including on overwrite:

```sh
kubectl memlens capture --volumes --pod app -n team-a --include-history -o incident.json
kubectl memlens replay incident.json
kubectl memlens replay incident.json --export-schema 1 -o legacy.json
```

Schema 5 is bounded to 2 MiB, 256 containers, 64 volumes and 181 optional history
points from the selected Pod/Node instance. Files are written atomically with
mode 0600; an existing file requires `--force`. Default captures replace names
with capture-local aliases and omit raw UIDs, runtime IDs, paths, labels, backend
messages and binding references. Aliases cannot link independent captures.
Explicit legacy exports to schema 1 or 2 omit volume and I/O enrichment and mark
the result partial with an omission caveat. Existing incident schemas 1/2/3/4
keep their domains, and older readers reject schema 5.

For a private comparison that needs identity continuity, explicitly use
`--include-sensitive` for both captures and protect those files. Compare with:

```sh
kubectl memlens compare --volumes --before before.json --after after.json
kubectl memlens compare pod/app-a pod/app-b -n team-a --volumes
```

Offline replay and comparison require no cluster access. Filesystem deltas need
matching verified binding references and fresh, ordered source samples. Memory
deltas additionally require the same Pod and container instances. Unlinked or
redacted observations remain separately labelled before/after evidence. Two
samples cannot establish sustained growth, latency or causality.

Disable `volumeContext.workloads` to remove its acquisition roles and route.
Before downgrading, disable the optional volume profiles and explicitly export
any incident needed by an older reader. There is no database migration.

## Aggregate operational measurements

The existing authenticated metrics resource adds fixed aggregate process and
retention measurements for qualification and operations. It adds no volume,
PVC, driver or caller labels and grants no new permissions:

- `kubememlens_collector_heap_objects_bytes` measures native Go heap objects.
- `kubememlens_volume_usage_enabled`, `volume_usage_entries`,
  `volume_usage_retained_bytes` and `volume_usage_commits_total` describe usage
  configuration and in-memory retention. Each name has the `kubememlens_` prefix.
- `kubememlens_volume_health_enabled`, `volume_health_entries`,
  `volume_health_retained_bytes`, `volume_health_payload_writes_total` and the
  `volume_health_backend_reads_total`, `failures_total` and `throttled_total`
  counters describe the health cache. Each full name starts with `kubememlens_`;
  backend counters share the `volume_health_backend_` stem.

The Node producer also exposes `kubememlens_node_context_posts_total` with fixed
success/failure labels and `kubememlens_node_context_last_post_seconds`. An
unobserved delivery duration is omitted. Delivery errors never enter metric text.
These counters measure operations, not storage health, disk persistence or
memory attribution. See [local volume qualification](volume-qualification.md).
