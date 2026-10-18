# Workload change markers

Optional change context puts selected workload changes beside source-labelled
memory trends. Correlation does not establish causation. Events are best-effort
reports with limited retention, not an audit log. This feature does not read
application logs, event messages, Secrets or arbitrary configuration values.

The standard profile remains capability-free. This opt-in profile uses ordinary
Kubernetes object and event reads; it does not require eBPF or Node access.
Managed-provider qualification remains separate from the implementation.

## Installation and access

First configure [memory history](memory-history.md), including its fixed verified
HTTPS provider, trust Secret and explicit namespaces. Then set:

```yaml
memoryHistory:
  markers: true
```

The chart adds namespace Roles and RoleBindings for the collector to read events
and named workload owners in those configured namespaces. It does not bind users
to the marker viewer or change existing viewer roles. Bind
`kube-memlens-history-marker-viewer` through a **RoleBinding in the intended
namespace**, alongside the existing Pod or workload history viewer. Custom roles
may restrict named Pod and workload access. Event listing requires a namespace
`list` permission; Kubernetes RBAC cannot restrict that list by involved object UID.
The API still filters events to the exact selected objects and verified owners.

Both `pods/trends-context` (or `workloads/trends-context`) and the existing named
`trends` permission are required. Ordinary object reads and every owner read are
also checked for the original caller, before acquisition and before disclosure.
Revoking a previously granted event permission prevents disclosure of that result.
A caller without event access can receive authorised Pod-state context with event
history explicitly marked `denied`.

Node history roles gain no permissions. Nodes have no marker endpoint. Setting
markers without enabled history and configured namespaces is rejected.

## Read and inspect

```sh
memlens history trends pod api -n team-a --source prometheus --markers
memlens history trends container api/app -n team-a --source local --markers
memlens history trends workload Deployment/api -n team-a --source prometheus --markers
```

In the TUI, select a Pod, container or workload, press `H` for history, then `m`
to toggle markers. Source switching, refresh and closing cancel outstanding
requests. Use `j`/`k`, Page Up/Page Down and `g`/`G` to navigate dense evidence.
The existing plain history response remains unchanged.

Markers retain object UIDs, verified controller chains and source timestamps.
Controller chains support Deployment, ReplicaSet, StatefulSet, DaemonSet,
ReplicationController, Job and CronJob. Custom, Node-owned or unknown-version
chains return an explicit unsupported-context error without broader acquisition;
plain history remains available.
Creation and restart markers come from API/runtime state. Resize conditions remain
pending or in progress; resize event reports distinguish started, completed,
deferred, infeasible and error. A reported event is not independent proof that an
operation succeeded. Current ReplicaSet revisions are labelled as observed context
with unknown change time; an old ReplicaSet's creation time is not a rollback time.

Same-name/different-UID events never join the current selected Pod. Source intervals
are preserved rather than replacing their timestamps with memory-grid timestamps.
Clock alignment and retention may be incomplete. A missing event list, a failed
source and a denied source remain distinct states. Absence of markers does not
prove that no change occurred.

## Capture, replay and compare

```sh
memlens capture trends pod api -n team-a --source prometheus -o history.json
memlens capture trends workload Deployment/api -n team-a --source prometheus -o workload-history.json
memlens replay history.json
memlens compare --trends --before before.json --after after.json
```

These commands use incident **schema 6**, separate from snapshot schema 6 and
unchanged incident schemas 1–5. Older binaries reject the new incident format.
Captures retain source clocks, marker counts, timestamps and caveats. By default,
names, UIDs, container IDs and owner identities become consistent local aliases.
Aliases from separate captures do not establish identity continuity. Comparison
therefore shows separate evidence and does not infer replacement from alias matches.

`--include-sensitive` explicitly retains raw authorised identities. With two such
captures, comparison can identify same-name/different-UID Pod replacement as an
observation interval when the later capture retains its verified owner chain. It
never splices memory series across UIDs or invents a one-to-one replacement for a
Deployment cohort with different Pod names. Source/metric mismatches are explicit.

Output files use mode 0600 and atomic publication. Existing files require `--force`;
an overwrite still performs a fresh authorised read. No cloud or cluster access
is needed for replay or file comparison. There is no lossy automatic conversion to
older incident formats.

## Bounds and failure behaviour

Memory keeps its 16-target, 241-point, seven-day and 1 MiB response limits. Markers
add at most 64 entries, three verified controller ancestors per entry and 128 KiB
of compact encoded marker evidence. The entire context response remains within 1 MiB.
Each workload acquisition/revalidation phase uses current authorised member
snapshots with the existing workload list permissions; per-Pod read checks remain.
CronJob reads reuse their bounded Job list and authorise the namespace Pod-list
operation once per phase. Every named Job read is still checked, and neither
permission decisions nor object snapshots are reused across phases or requests.
Context target resolution also uses these authorised membership snapshots, then
resolves the complete selection afresh before disclosure. Its existing five-second
deadline and 20-operation/second limiter remain unchanged. Plain history keeps its
existing object-read path.
The source reads one page of at most 256 events (plus one overflow sentinel) within
a shared 4 MiB object budget. Additional pages are not silently treated as complete;
truncation is explicit. That page may omit newer events. Within acquired evidence,
markers are ordered deterministically and the most recent 64 are retained.

Marker acquisition and revalidation each have a two-second deadline, a bounded
40-operation/second limiter with an 80-operation burst, and one in-flight operation.
The opt-in API/client have 12/13-second deadlines; existing plain history stays at
9/10 seconds, and ordinary live reads keep their separate admission and timeout.
Slow or invalid context does not silently turn into plain history. Captures are
bounded to 2 MiB including their provenance envelope and formatted JSON.
Capture decoding removes formatting whitespace before applying the unchanged
nested evidence budgets; string contents and duplicate-key checks are preserved.

## Rollback

The [local qualification record](qualification-results/memory-change-markers-local-kind-2026-09-29/README.md)
records the tested runtime paths and their limits.

Set `memoryHistory.markers: false` and upgrade. The context endpoints, collector
flag and optional marker Roles/RoleBindings disappear; ordinary history remains.
Remove any operator-created RoleBindings for the marker viewer. Keep a schema-6
reader for existing captures. Removing the feature does not rewrite their evidence.
