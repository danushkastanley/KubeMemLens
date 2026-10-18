# Optional memory trends

The existing `history pod` and `history node` commands keep their current local
retention semantics. The optional `history trends` path compares those retained
sources with an operator-owned Prometheus-compatible service. It does not install
Prometheus, add a database, change exporter labels or combine unlike memory values.

The development feature passed [local functional qualification](qualification-results/memory-history-local-kind-2026-09-28/README.md). Managed-provider qualification remains separate.

## Supported remote source

Use the Prometheus v1 `POST /api/v1/query_range` API over verified HTTPS. A base URL
may include a reverse-proxy path. Redirects, URL credentials and arbitrary reader
queries are rejected. The optional bearer token must grant read-only access to the
selected remote service. Provider failure does not occupy the live-memory read
queue or automatically switch the meaning of a result.

The initial profile supports `container_memory_working_set_bytes` and
`container_memory_rss`, with cAdvisor-compatible byte semantics. Working set and
RSS overlap other memory fields; neither is cgroup charged total. For Nodes, only
the root cgroup (`id="/"`) is queried, with empty namespace, Pod and container
labels. Node history never includes Pod contributors.

Every accepted series must have these exact identity labels:

| Scope | Required labels |
| --- | --- |
| Container | `cluster`, `node`, `node_uid`, `namespace`, `pod`, `pod_uid`, `container`, `container_id` |
| Node root | `cluster`, `node`, `node_uid`, `id="/"`; `namespace`, `pod`, `container` absent or empty |

`container_id` is the full Kubernetes container-status ID, including its runtime
prefix. UIDs must describe the instance at the time the sample was acquired.
The operator must maintain these labels from immutable source identity. Do not
join old name-only samples to current Kubernetes UIDs, and do not rewrite earlier
container instances to the latest container ID. Duplicate scrape series or nested
cgroups carrying the same identity are rejected instead of summed.

These identity labels are **not** added to KubeMemLens's own metrics. A typical
unmodified scrape that lacks them produces unreported history. The adapter cannot
distinguish absent labels from absent retention without widening its query scope.

Pod queries return separate currently running container-instance series. Stopped
containers and earlier instances are outside this profile; a selected container
with no running identity is unavailable. These series are not a full Pod total.
Workload queries
resolve the current controller UID/generation and authorise each current member;
previous replicas are excluded. Every response rechecks the selected identities
before returning. Pod or container replacement requires a fresh selection.

## Bounds and evidence

| Limit | Value |
| --- | --- |
| Current targets per query | 16 |
| Points per series | 241 |
| Maximum requested window | 7 days |
| Grid step | 1 minute to 1 hour |
| Encoded provider request | 32 KiB |
| Provider response and API response | 1 MiB each |
| Provider queries in flight | 1 |
| Provider deadline | 5 seconds; no retry |
| Whole API deadline | 9 seconds, including identity revalidation |
| History client deadline | 10 seconds; earlier caller cancellation wins |

The default remote window is 24 hours; the default local window is 15 minutes.
The grid step grows to keep the response within the point limit. It describes
query resolution, not an inferred scrape interval. Actual retention
belongs to the source and may be shorter. Missing samples, NaN values, stale
samples and reported zero remain distinct. Provider warnings mark evidence
partial; their raw text is never returned or logged.

The initial response profile accepts at most 32 labels per returned series, with
ASCII Prometheus label names up to 128 bytes and values up to 1,024 bytes. Long
identity labels can reach the request-byte limit before the target-count limit;
use a smaller Pod or container selection when that happens.

A single expression obtains both each value and its original Prometheus sample
timestamp. The evaluation grid is not treated as a scrape timestamp. Remote
samples older than two minutes at an evaluation point are marked stale. Local
Pod history uses the collector's continuity interval, capped at two minutes;
local Node history uses the Node source's stale interval. The response records
its freshness interval and sample clock. The terminal view ages retained evidence
without silently refreshing it.

The query adapter has been exercised against a local Prometheus 3.15.0 evaluator
over verified TLS, including gauge decreases, zero, instance replacement and
duplicate-series rejection. This host check does not establish Kubernetes or
managed-provider qualification, or compatibility with other implementations.

To repeat that check, independently verify a Prometheus binary for your host
against its upstream release checksums, then run:

```sh
mkdir -m 700 /tmp/memory-history-evidence
go run ./hack/memory-history-prometheus \
  --prometheus-binary /path/to/verified/prometheus \
  --evidence-dir /tmp/memory-history-evidence
```

The verifier creates a fresh private directory, binds synthetic metrics and
Prometheus only to loopback, runs the production adapter, and stops Prometheus.
It retains the binary hash, results, logs and short-lived fixture TLS material;
the directory is local evidence and must not be committed. It uses no Kubernetes
credentials and creates no cluster workloads.

Explicit local fallback returns Pod cgroup charge, or retained Node Summary
working set/RSS. Disabled Node collection is reported as disabled, separately
from missing samples. It never creates per-container history from a Pod sum. Gaps,
collector restarts and retention loss stay visible. Existing local history APIs
and incident schemas are unchanged.

## Installation and permissions

Start from [the optional profile](../charts/kube-memlens/profiles/memory-history.yaml).
Set an exact HTTPS URL, cluster label and namespace allow-list. Enable Node or
workload scope separately when needed. Create the referenced Secret in the
collector namespace with only the selected trust and credential keys. The chart
mounts those keys read-only; it never accepts inline credentials. Credential
changes require a collector rollout. A CA bundle is required even for a public
certificate chain: the scratch image has no system CA store.

The profile adds namespace-scoped acquisition roles for the collector and leaves
all existing viewer roles unchanged. Keep the normal memory viewer binding for
the TUI; the additional history roles do not grant general memory list access.
New viewer roles are unbound:

- `kube-memlens-history-viewer`: bind with a RoleBinding in an allowed namespace.
- `kube-memlens-workload-history-viewer`: additionally permits current owner and
  member resolution in the bound namespace, when workload scope is enabled.
- `kube-memlens-history-node-viewer`: grants Node-only history; bind only to readers
  intended to have cluster-level Node visibility. It does not grant the separate
  collector inventory endpoint or its container counts.

Pod history reuses the collector's authenticated Node coverage identity. Only
explicit Node scope adds a separate `get nodes` acquisition role. Workload scope
adds bounded controller reads; it adds no volume, secret, exec or mutation rights.
The reader's resource permissions are checked separately from the collector's
acquisition permissions. NetworkPolicy remains the existing chart policy; operators
should constrain remote egress with their cluster's network controls where needed.

## Operator workflow

```sh
kubectl memlens history trends pod api -n team-a --source prometheus
kubectl memlens history trends container api/app -n team-a --source prometheus --metric rss
kubectl memlens history trends workload Deployment/web -n team-a --source prometheus
kubectl memlens history trends node worker-a --source prometheus
kubectl memlens history trends pod api -n team-a --source prometheus --window 168h
kubectl memlens history trends pod api -n team-a --source local
```

Use `--output json` or `--output yaml` for the bounded report; authorised names
and immutable identities are included. No incident capture format is changed.

In the TUI, select a Pod, container, workload or Node and press **H**. Use **p**
for Prometheus, **l** for an explicit local fallback, **r** to refresh and **Esc**
to return to live evidence. If the selected instance changes, close history,
refresh the selection and reopen it. Scroll with arrows or page keys. Gaps remain `.` and
stale points remain `!`, including when a plot is compressed. Each series uses its
own visual scale; compare labelled byte values, not chart height across series.

Removing the profile removes its read path, acquisition roles and trust mount.
It leaves existing collection, local history and the external Prometheus service
unchanged. See [ADR 0017](adr/0017-query-identity-bound-memory-history.md).
