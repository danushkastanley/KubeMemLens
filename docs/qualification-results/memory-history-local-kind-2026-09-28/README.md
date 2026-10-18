# Local memory-history qualification — 28 September 2026

The optional history adapter passed the local functional checks recorded in
[summary.json](summary.json). The runtime used two kind Kubernetes 1.37.0 arm64
Nodes, LinuxKit 7.0.12 and containerd 2.3.4. This is our own local evidence;
managed-provider qualification remains separate.

## Candidate and reproducibility

The tested worktree was based on `d6670e57def2023668fe7bb9fc58e57dcaf478b0` with
the uncommitted history feature. [source.json](source.json) records every
production `cmd/` and `internal/` Go input plus `go.mod` and `go.sum`. The summary
records the canonical source fingerprint, five Linux binary hashes and the local
candidate/fixture image IDs. These image IDs are evidence identities, not public
registry references. No dependency was added for the production feature.

Host checks covered the full root race suite, vet, module verification, both Linux
architectures, decoder fuzzing, bounded response/load cases, CLI requests and TUI
state/size behaviour. Native Linux arm64 race and vet checks covered the history,
Kubernetes, API, collector, client, CLI and TUI paths after the final authorisation
change. Follow-up tests verified the named history subresource reaches delegated
authorisation and the last series' sample clock remains reachable at every
accepted terminal size.

The real Prometheus expression was separately exercised against checksum-verified
Prometheus 3.15.0 on Darwin arm64 over verified HTTPS, including decreasing gauges,
zero, sample timestamps, Node root selection, replacement and duplicate-series
rejection. The fixture certificate chain also passed strict X.509 verification.
See [the repeatable command](../../memory-history.md#bounds-and-evidence).

## Local runtime results

The runtime installed the actual chart and candidate binaries. A controlled HTTPS
range API supplied synthetic measurements labelled with the fixture workloads'
real Kubernetes identities. Forty-eight retained checks cover:

- Pod, container, Node and all seven supported workload-owner kinds;
- namespace, named-resource, core-object and Node-only permissions, with denied
  requests never reaching the provider;
- exclusion of the separate Node inventory/container-count endpoint from the
  Node-history role;
- explicit local Pod charge and disabled local Node collection;
- warm and in-flight revocation, including revoking only `pods/trends` while
  ordinary object access remains;
- provider timeout/cancellation, independent live reads, concurrent-history
  rejection, malformed responses, wrong identities and duplicate series;
- Pod deletion/name reuse, source recovery and actual CLI JSON workflows;
- sixteen targets with 241 points each, and rejection of a seventeenth target
  before a provider query;
- real terminal source switching and return to live diagnosis at 80×24, 120×36
  and 160×48; maximum-size rendering/scrolling was separately tested in the TUI;
- upgrade/disable, removal of extra roles/flags/trust mounts, revoked-reader 403,
  administrator-confirmed route removal (404), and continued current live reads.

The full-size API request completed in 5.90 seconds in this run. This is a
functional deadline observation, not a throughput or tail-latency benchmark.
The owned history release, fixture namespaces and memory APIService were removed;
the shared local cluster remains available for the remaining product tests.

## Repairs and limits

Failed harness attempts were retained in local evidence. They exposed a noexec
test-binary mount, missing strict-verifier certificate identifiers, a recorder
that did not accept a disabled report's null series, an incremental-terminal
redraw assumption, and use of a revoked reader where an administrator was needed
to prove route removal. These were corrected without disabling TLS verification
or changing product rejection limits.

Before runtime acceptance, code review also found and removed an unnecessary
Node-inventory permission. Named history permission is now rechecked before
acquisition and before disclosure; live tests confirm that revocation discards
an in-flight result.

The local source uses synthetic values. It does not establish cAdvisor label
enrichment, external retention, other Prometheus-compatible implementations,
managed-provider support, independent review or adopter outcomes. The initial
[source and permission contract](../../memory-history.md) remains the boundary.
