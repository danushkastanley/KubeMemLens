# Agentless current observations

The restricted reader obtains authorised Pod and Node working sets and resource
context without installing KubeMemLens workloads. `EvidenceSession.Observations`
exposes the common `Current` query. The existing deep snapshot interfaces and
collector wire formats remain unchanged. [Restricted CLI/TUI workflows](restricted-mode.md)
consume these observations separately from cgroup renderers. Incident export is
a subsequent delivery step.

## Scope and identity

The reader uses the selected kubeconfig and context directly. Namespace mode
lists only that namespace's Pods and PodMetrics. Explicit all-namespace mode
uses cluster list endpoints; it never substitutes a series of namespace reads
when cluster access is denied. Discovery is a hint, and every refresh makes new
authorised requests.

Pod inventory is required. A denied or invalid later page discards earlier Pod
pages. Optional metrics, owner and Node failures retain valid Pod resource rows
with explicit missing evidence. The Pod read gets the request budget before
optional enrichment. An optional deadline returns a partial frame when the Pod
inventory is already complete; caller cancellation returns an error.

ReplicaSet and Job resolution uses deduplicated, bounded GETs in the Pod's
namespace. The lookup binds the reference's API group and UID when present.
A fresh resolver per refresh prevents a cache from retaining revoked owner
access. Namespace Node context uses GETs only for Nodes referenced by visible
Pods. Explicit cluster mode lists Nodes. NodeMetrics has independent permission
and API availability; a failed Node source cannot broaden the Pod scope.

No path uses kubelet access, `nodes/proxy`, host mounts, an administrator's
collector, permission changes or cloud provisioning.

## Memory and metadata

`WorkingSet.Bytes` is nullable. A measured zero has a value; unavailable memory
has no value. Working set never populates `MemoryBreakdown.TotalBytes`, cgroup
composition, PSI, reclaim counters or local OOM deltas. Kubernetes termination
status is preserved separately from cgroup event counters.

Inventory starts with Pod specs, so pending Pods and application, init and
ephemeral containers remain visible even without metrics or runtime IDs.
Requests, limits, resize context, hugepages, owner, phase and creation time come
from the Kubernetes objects. Creation and condition transition times are not
used as metric sample times. Labels are carried once per Pod.

Metrics join on namespace, Pod and container name. A reported UID must match;
samples older than Pod creation or the current container start are rejected.
An absent provider UID leaves a visible identity caveat and partial evidence.
Each value carries its Metrics API version, timestamp, receive time, sample
window and freshness. Valid memory survives missing CPU usage; CPU's separate
`cpuUsageKnown` field remains false.

Pod, workload and namespace sums retain observed/expected container coverage and
the oldest/latest sample times. Missing containers do not become zero. A bounded
Pod inventory marks its groups partial. Incompatible sources and overflowing
sums are unavailable. Full Node working set is independent of visible Pod sums;
capacity, allocatable and MemoryPressure are independently reported Node status.
Allocatable minus working set is not presented as free memory.

## Bounds and privacy

Default per-refresh bounds are:

| Bound | Default |
| --- | ---: |
| Whole request deadline | 5 seconds |
| In-flight requests | 4 |
| Requests | 1,100 |
| Response / combined response bytes | 4 MiB / 32 MiB |
| Encoded observation output | 16 MiB |
| Page size / pages per list | 500 / 4 |
| Pods / containers / Nodes | 2,000 / 10,000 / 500 |
| Distinct owner reads | 500 |

Options validate fixed upper ceilings. Refreshes on one reader are serialised
and share no result cache. Repeated cursors, changed inventory resource versions,
duplicate identities, redirects, negative quantities and numeric overflow are
rejected. Hitting the output bound returns an error without a result.

The application model contains authorised object metadata, so it is not a
redacted export. It contains no kubeconfig, token or TLS key. Typed failures
expose bounded reasons without raw API response text. Local verification writes
only sanitised assertions, never observation payloads or credentials.

## Local verification and rollback

```sh
AGENTLESS_KUBECONFIG=/path/to/kind-kubeconfig \
AGENTLESS_CONTEXT=kind-example \
AGENTLESS_ARTIFACT_DIR=/path/to/private-results \
AGENTLESS_ACKNOWLEDGE=run-and-remove-agentless-fixture \
  hack/verify-agentless-kind.sh
```

Use a disposable kind cluster without a KubeMemLens or Metrics API installation.
The script refuses to replace existing fixture resources. It creates two data
namespaces, a controlled TLS Metrics API in a third namespace and a short-lived
reader credential. It verifies missing and measured memory, namespace and
cluster denial, auto selection, CLI/PTY workflows and revoked access, then removes the resources
and credentials. The controlled samples prove the integration and join contract,
not Metrics Server accuracy or managed-provider support.

Revert the reader change to remove restricted current queries. Existing deep
collection and stored data require no migration or rollback operation.
