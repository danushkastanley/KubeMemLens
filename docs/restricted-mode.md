# Restricted memory workflows

The development build can inspect caller-authorised Kubernetes working sets and
resource/status context without a KubeMemLens collector. It uses the
[agentless reader](agentless-reader.md) and the same namespace, workload, Pod,
container and Node navigation in the TUI. Local verification does not establish
support for a managed-provider profile.

```sh
kubectl memlens --mode=restricted top pods -n my-namespace
kubectl memlens --mode=restricted top containers -n my-namespace -o json
kubectl memlens --mode=restricted top workloads -n my-namespace
kubectl memlens --mode=restricted top ns -n my-namespace
kubectl memlens --mode=restricted explain pod my-pod -n my-namespace
kubectl memlens --mode=restricted recommend pod my-pod -n my-namespace -o yaml
kubectl memlens --mode=restricted tui -n my-namespace
```

Automatic mode first tries the relevant deep query. It can select restricted
mode when the deep API is absent or access is forbidden. Selected-Pod commands
probe that Pod, so deep callers with `get` permission do not acquire a namespace
`list` requirement. A missing deep Pod is an object error, not proof that the
deep API is absent. Explicit deep mode and collector URL/proxy commands retain
their original readers. See [source selection](evidence-sources.md).

## Evidence and permissions

Restricted memory is Metrics API **working set**. It does not supply cgroup
charge or composition, local OOM deltas, PSI, reclaim counters or trace data.
Kubernetes restart and termination status is labelled separately. Missing
metrics retain resource and status rows; they never become a measured zero.

Values retain the API version, sample time, window, freshness, identity caveats
and reported/expected coverage. Groups sum visible observations and remain
partial when containers are missing. Node working set is independent of the
sum of visible Pod working sets. Source age stays visible in the TUI, including
compact frames; the source line shows the oldest available frame sample.

The caller needs `list` on core Pods in the chosen namespace. PodMetrics reads
use separately authorised `list` access in `metrics.k8s.io`. Optional owner
resolution needs scoped ReplicaSet/Job GETs. Namespace Node context uses GETs
only for referenced Nodes, with separate NodeMetrics permission. `-A` explicitly
requests cluster reads; denial never becomes a sequence of namespace reads.
No command installs agents, Metrics Server, RBAC or other resources.

Deep mode depends on cluster policy and requires authorised collector and node
cgroup access. The UI does not promise that a restricted provider permits it.

## Navigation and failures

The TUI keeps its existing view keys, drill-down, selection, scrolling, filters,
pause, manual refresh and split detail. Restricted sort cycles between working
set and name. Text, `owner:`/`workload:` and `state:` filters use available facts.
Severity, diagnosis and pressure filters return an explicit deep-evidence reason.
`NO_COLOR` remains supported.

Transient query failures retain the last frame and source age. Permission or
authentication loss clears retained data, selections and pending actions, as in
deep mode. Restored access can be refreshed through the same reader. CLI watch
also retains transient frames, clears lost-access frames and stops if output
closes.

Read-only recommendations explain what the source can establish and offer
investigation commands. They do not infer leak, reclaim or OOM risk from absent
deep fields. Copied TUI commands preserve explicitly selected kubeconfig/context
options. Other suggested commands should be run with the same caller settings.

History still requires deep evidence. Restricted capture, replay and comparison
are not implemented in this build. The action menu shows those limits and never
opens a capture destination prompt for an unsupported path.

## Machine output

Deep top rows and explanation/recommendation output remain unchanged. Restricted
top rows add `mode`, `scope`, `workingSet`, source timestamps and optional resource
context. `workingSet.bytes` is nullable. Missing cgroup composition fields are
omitted rather than supplied as zero. Table and CSV output identify the selected
mode and source; sort names `total`, `name` and `namespace` are supported, with
`total` meaning the reported working-set sum in restricted mode.

Restricted explanation schema **4** and recommendation schema **2** contain a
target, source-aware observation, children, source reports, unavailable queries
and read-only guidance. Deep output remains explanation schema **3** and
recommendation schema **1**. Consumers must reject unsupported versions. No
collector snapshot or incident schema changes are introduced here. See the
[explanation contracts](explanation-schema.md#restricted-evidence).

These outputs exclude arbitrary labels, Pod UID, container ID, cgroup paths,
credentials and raw Kubernetes objects. Names and namespace/node context remain
available for the authorised operator; this is not a redacted incident export.

## Verification and rollback

`hack/verify-agentless-kind.sh` now exercises real CLI output and the TUI in
80×24, 120×30 and 180×50 pseudo-terminals before a collector is installed. The
fixture covers missing/measured memory, navigation, filtering, sorting,
pause/refresh, detail, recommendations, unavailable actions, permission
revocation/recovery and terminal restoration. It retains sanitised assertions,
not raw PTY output or credentials. Source/row and state-machine tests also cover
40×10, measured zero, stale frames, output failure and deep compatibility.

Revert the presentation change to restore the previous command boundary. There
is no persisted-data or cluster migration. Where available, `--mode=deep` keeps
the existing deep workflow.
