# Node memory analysis

Node analysis combines source-labelled facts with authorised cgroup contributors.
It is read-only. The pure `internal/nodeanalysis` module is shared by the
collector API and client-facing workflows; collection remains independent.

## Accounting rules

Usage, working set, RSS, available memory, capacity and allocatable remain
separate facts. Never add overlapping values to create a memory partition.
Kubernetes derives its Linux eviction availability from cgroup information,
excludes inactive file memory, and can adjust for hugepage capacity. This is not
an interchangeable definition of raw cgroup charge or physical free RAM.
See [Node-pressure eviction](https://kubernetes.io/docs/concepts/scheduling-eviction/node-pressure-eviction/).

The default result has no gap estimate. An operator-owned qualification must
establish that the exact Node boot's Summary usage includes the same charge
accounting as the observed container cgroups. Both sources must be fresh and
within five seconds of each other. The cgroup frame must have complete reads and
mapping, no missing agent, no identity mismatch and no arithmetic overflow.

The outside-observed-Pods estimate is:

```text
max(0, node.usage - sum(mapped container memory.current))
```

It includes system memory and unobserved overhead; it is not an application leak
or an exact accounting claim. The stronger unaccounted estimate additionally
requires explicitly qualified disjoint system categories:

```text
max(0, node.usage - observedPodCharge - qualifiedDisjointSystemUsage)
```

The Summary `pods` category cannot be subtracted as system memory because it
overlaps the observed Pod charge. Kubelet, runtime and misc values are retained
as separate facts; their presence alone never proves disjointness. Each selected
system measurement must satisfy the same freshness and five-second alignment
checks. Negative differences floor to zero, retain a sample-skew caveat and lower
confidence. Checked arithmetic suppresses an estimate when a sum overflows.

Hugepage pool capacity and allocatable are shown separately. They are not ordinary
headroom or current hugepage consumption. No hugepage value is added to or
subtracted from a reported memory value by this module. Kubernetes exposes
hugepages as distinct resources that cannot be overcommitted; see
[Manage HugePages](https://kubernetes.io/docs/tasks/manage-hugepages/scheduling-hugepages/).

## Severity and contributors

These are KubeMemLens triage thresholds, not universal kernel thresholds or
changes to kubelet eviction policy:

| Fresh evidence | Severity |
| --- | --- |
| Kubernetes `MemoryPressure=True` | Critical |
| Node memory PSI full avg10 at least 10%, or full avg60 at least 1% | Critical |
| Node memory PSI full avg10 at least 1%, or some avg10 at least 10% | Warning |
| Observed Pod OOM kills in a known recent cgroup interval | Warning; this does not prove a Node-wide OOM |
| Reported low Node PSI or `MemoryPressure=False`, without stronger evidence | Normal for the available evidence |
| No usable pressure evidence | Unknown |

PSI measures stalled execution time. The kernel distinguishes partial stalls
from intervals in which all non-idle tasks are stalled, with 10/60/300-second
averages. The product thresholds above interpret those measurements; they are
not prescribed by the kernel. See [PSI documentation](https://docs.kernel.org/accounting/psi.html).

Swap allocation alone never raises severity. Growth is labelled as allocation,
not swap I/O. Major-fault rates can include file-backed faults and do not establish
swap activity. Derived values retain their formula, source and sample interval;
counter rates require a matching Node UID and boot, increasing timestamps and
monotonic counters. A gap longer than two minutes produces no current rate.

Pod and workload rankings support `total`, `anon`, `cache`, `shmem`, `residual`,
`psi` and `oom`. Bytes and events use checked sums. PSI ranking uses the maximum
observed container full avg10; percentages are never summed. Ties use namespace,
kind, name and UID. Responses return 20 rows by default and at most 100 per
ranking level. Unmapped containers reduce coverage and exclude a qualified gap.

## Authorisation and API

The opt-in Node viewer role includes `get nodecontexts/analysis`. That permission
allows Node facts only. Before looking up contributors, the collector performs
an uncached, cluster-scoped authorisation check for `list pods` in the
`memory.kubememlens.io` API group. A namespace RoleBinding is insufficient.

A denied secondary decision returns Node-only analysis with no contributor names,
counts, coverage or Pod-charge total. A failed authorisation backend returns an
error. Revocation therefore replaces a previously authorised result on the next
read, rather than leaving contributor data in a fresh response.

Advertise snapshot schema 3 and request:

```text
GET /apis/memory.kubememlens.io/v1alpha1/nodecontexts/NAME/analysis?rank=total&limit=20
X-KubeMemLens-Snapshot-Schema: 3
```

The response contains analysis schema 1, independent of incident schemas. Node
and cgroup records are sampled atomically inside the store and joined by current
Node UID. The existing response ceiling still applies.

Use the authenticated Kubernetes API connection to explain one Node or inspect
its retained history:

```sh
kubectl memlens explain node worker-a --rank psi --limit 20
kubectl memlens explain node worker-a --output json
kubectl memlens history node worker-a --since 5m
kubectl memlens history node worker-a --output yaml
```

These commands request only Node resources. A Node-only viewer can read source
facts without first listing Pods. The explanation reads analysis last so its
contributor permissions are checked for that command. If the Node identity or
source sample changes between requests, the command asks for a refresh rather
than combining inconsistent evidence.

History traverses at most eight Node instances, with at most 61 points per
instance. A failed page, changed collector generation or invalid continuation
aborts the read. Multiple retained Node instances remain partial coverage.
`--since` filters by source sample time within the retained 15-minute window;
the displayed coverage metadata still describes the collector's whole window.

See [Node incident capture and replay](node-incidents.md) for redacted schema-4
exports and compatible offline comparisons.

## Terminal cockpit

In the deep-mode TUI, press `N` to select the Node table and `e` to open a Node's
scrollable detail. The detail includes independent memory values, source ages,
completeness, swap, PSI, faults, system categories, hugepage pools, observed charge,
qualified estimates, contributors and retained history. Use arrows or `j/k`,
Page Up/Down and `g/G` to reach every field at 80x24. At 160x35 and wider layouts,
the selected Node's memory and contributors appear beside the table; `Tab`
switches focus to that scrollable pane.

`s` cycles total, anon, cache, shmem, residual, PSI and OOM contributor rankings.
`Enter` opens the existing Pod view filtered to the Node and the caller's scope;
`h` returns to the Node table. `C` opens a redacted Node capture destination, and
`y` copies the Node explanation command. Text labels carry state without relying
on colour; Node detail also works with `NO_COLOR`.

Only the selected Node is polled. Selection changes and pause cancel its pending
request; late responses cannot overwrite a new selection. Space pauses Node
polling and resumes it with a new read. An explicit capture still obtains fresh
authorisation while polling is paused. Failed Node/history requests show the
last-good Node-only evidence with stale/error labels; cached contributor rankings
and Pod-derived severity are removed when their current read fails. Forbidden
or missing resources clear the corresponding retained data. If the optional
profile is disabled or unsupported, the observed-charge Node view remains the
fallback.

## Operator accounting qualifications

Mount an operator-reviewed ConfigMap containing `qualifications.json` and set
`nodeContext.accountingConfigMap` to its name. The collector reads it at startup;
a changed ConfigMap requires a collector restart. It does not receive this policy
from producer data or read query parameters.

The file is a JSON array of at most 1,000 records, at most 2 KiB per record and
1 MiB overall. Each record contains:

| Field | Meaning |
| --- | --- |
| `nodeUID` | Exact current Kubernetes Node UID |
| `nodeStartedAt` | Exact Summary Node start time for the qualified boot |
| `validFrom`, `expiresAt` | Explicit UTC validity interval, at most seven days |
| `evidenceSHA256` | Digest referring to independently reviewed accounting evidence |
| `usageDefinition` | `cgroup-charge-inclusive` |
| `disjointSystems` | Optional unique subset of `kubelet`, `runtime`, `misc` |

A digest is an evidence reference, not an automatic certification. The operator
is responsible for reviewing the referenced evidence. Node replacement, a new
boot or expiry invalidates the qualification. The operator must also remove it
after runtime, kubelet, producer or accounting-configuration changes, even if the
Node UID and boot time remain unchanged. No
qualification file is shipped by default. The local kind fixture verifies paired
source handling and authorisation without claiming host-accounting compatibility.
Managed-provider qualification remains a separate gate.

Rollback removes the qualification setting or disables the optional Node-context
profile. No storage migration or workload resource change is needed.
