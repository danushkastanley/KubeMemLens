# Volume context contract

The volume context model keeps filesystem usage, memory-backed configuration
and CSI health separate. It does not add volume bytes to cgroup memory charge.
The contract is implemented in `internal/volumecontext`; collection and user
interfaces are not enabled by the model alone.

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

No access endpoint or additional RBAC is installed by this contract change.

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
| Future collector volume retention | 64 MiB total including last-good records and indexes |
| Usage freshness / expiry | Stale after 45 seconds; omitted after two minutes |

Encoded-byte limits apply in addition to field counts. A maximum count of
maximum-sized fields is not guaranteed to fit. Inputs exceeding either ceiling
are rejected explicitly. The private decoder rejects unknown fields, duplicate
keys, case aliases, oversized collections, excessive nesting, invalid integers
and trailing JSON before admitting records. Error text contains no identities.

The existing Node observation stays limited to 16 KiB. Volume batches are
separate from Node memory. Integration must preserve the 4 MiB Summary-response
ceiling, existing timeout/interval and single-request concurrency. The storage
and pagination constants define integration ceilings, not a measured scale
claim or an already implemented volume store.

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

The volume wire contract is version 1. Snapshot schema 4 is reserved for future
volume integration but is not yet advertised. Snapshot schemas 1/2/3 continue
unchanged. Incident schema 5 is reserved; existing readers still reject it until
capture and replay integration exists. Deep incident schemas 1/2, restricted 3
and Node 4 keep their meanings. Integration must explicitly project unsupported
fields for old readers and report omitted evidence in legacy captures.

The model has no runtime switch or database migration. Removing it removes only
the unused enrichment contract. Collection integration must allow disabling
volumes without disabling Node or cgroup memory before collector downgrade.
