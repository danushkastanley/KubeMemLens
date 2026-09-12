# Volume context contract

The volume context model keeps filesystem usage, memory-backed configuration
and CSI health separate. It does not add volume bytes to cgroup memory charge.
The optional collector API provides current Pod volume configuration and kubelet
filesystem measurements. Collection defaults to off; CLI/TUI presentation and
volume incident capture are separate integrations.

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
Staleness does not remove adverse evidence. A health transition is not a probe
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
| Collector volume retention | 64 MiB total including last-good records and indexes |
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
batches and enables the named Pod-volume resource. Schemas 1/2/3 omit the new
batch and keep their existing representations; the new route is hidden from
readers that negotiate an older schema. Incident schema 5 is reserved; existing readers still reject it until
capture and replay integration exists. Deep incident schemas 1/2, restricted 3
and Node 4 keep their meanings. Integration must explicitly project unsupported
fields for old readers and report omitted evidence in legacy captures.

No durable database migration is required. Disable `nodeContext.volumeStats`
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
in one terminal. In another, explicitly negotiate schema 4:

```sh
curl --fail --silent --show-error \
  --header "X-KubeMemLens-Snapshot-Schema: 4" \
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
usage. This endpoint currently supplies filesystem usage, not CSI health.

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
