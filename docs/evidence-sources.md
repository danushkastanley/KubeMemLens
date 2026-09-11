# Evidence source discovery

`status` and the TUI resolve evidence sources through a shared plan before
rendering data. Mode describes the source; freshness and completeness describe
the evidence. A reachable source can still be partial, stale or not yet sampled.

```sh
kubectl memlens status -n my-namespace
kubectl memlens status -n my-namespace --output=json
kubectl memlens --mode=deep tui -n my-namespace
kubectl memlens --mode=restricted status -n my-namespace
```

`--mode` accepts `auto`, `deep` or `restricted`. `--connect-mode` still selects
the collector transport. Explicit collector URL, HTTP or proxy selections stay
on deep mode; they cannot be combined with explicit restricted mode.

Auto mode prefers the deep source when it can read the requested scope. Stale or
partial deep evidence stays labelled as such. When the aggregated API is absent
or access is forbidden, auto mode can discover separately authorised Kubernetes
status and resource-metrics sources. An explicit deep selection never falls back.
Authentication failure, malformed responses and connection failure remain errors.

Restricted discovery and the [agentless current reader](agentless-reader.md) are
available in this build. Restricted interactive workflows and capture are
separate delivery steps; their commands still return `query-not-implemented`.
Discovery does not install agents, Metrics Server,
RBAC or other cluster resources. No restricted-provider support is claimed.

## Reading the report

The optional `evidence` object in status JSON records mode, source state,
freshness, completeness and query capability. Each source includes a bounded
reason and the served API version where known. Kubernetes working set is distinct
from cgroup charge. Status does not report measurements from API discovery alone.

Absent API, forbidden access, unsupported API contract and unavailable transport
remain separate. API permission probes have unknown freshness and partial
completeness until a reader obtains observations. An empty deep response likewise
does not prove that the requested scope has complete memory coverage.

Query availability means the selected path implements the operation with that
kind of evidence. It does not promise that the caller may read every endpoint.
History, Node and collector metrics operations still enforce their separate
permissions when invoked. Scope is fixed for the lifetime of a reader.

Status without `-n` retains the cluster-wide check and collector store report.
`status -n` checks only that namespace and omits cluster store counts. The TUI
shows the shared mode/source label below its existing status header, including
at compact sizes. Discovery failures support refresh/retry; successful selection
does not change sources during an ordinary refresh failure.

## Bounds and privacy

Discovery has a five-second default whole-operation deadline and a fixed probe
sequence. Kubernetes self access reviews request only `list` on `pods` in the
core or metrics group, in the exact requested namespace or explicit all-namespace
scope. They do not enumerate workload names or discover other namespaces.
Reviews are bounded to 1 MiB, reject redirects and discard free-form reason text.
Metrics discovery keeps the [existing transport and response bounds](resource-metrics-source.md).

No credentials or object identities enter the public selection. A successful
self access review is only a hint; later reads enforce current Kubernetes RBAC.
No collector data format, capture schema or chart permission changes are needed.
See [ADR 0006](adr/0006-select-evidence-sources-before-rendering.md).

## Local verification

`hack/verify-evidence-discovery-kind.sh` uses an existing kind cluster. It creates
one disposable namespace, a Pod-list-only reader and a short-lived token, then
removes them. It verifies namespace-scoped restricted discovery, auto selection,
explicit deep denial, cross-namespace denial, cluster-scope denial and sanitised
reports. It installs no workloads and leaves the existing collector unchanged.

```sh
EVIDENCE_KUBECONFIG=/path/to/kind-kubeconfig \
EVIDENCE_CONTEXT=kind-example \
EVIDENCE_CLI=/path/to/kubectl-memlens \
EVIDENCE_ARTIFACT_DIR=/path/to/private-results \
EVIDENCE_ACKNOWLEDGE=run-and-remove-evidence-fixture \
  hack/verify-evidence-discovery-kind.sh
```

The fixture requires the deep API to be installed so its scoped denial can be
verified. The [agentless harness](agentless-reader.md#local-verification-and-rollback)
verifies data queries without a collector. This harness proves discovery and authorisation, not provider support.
