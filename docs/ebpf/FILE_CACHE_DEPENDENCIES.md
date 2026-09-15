# Incident worker dependency inventory

Date: 15 September 2026. Scope: the candidate
`prototype/trace/worker/cmd/memlens-filecache-worker` entrypoint, with Go 1.27.1,
Inspektor Gadget v0.56.0 and the recorded SDK policy patch. This inventory is not
maintainer acceptance, an independent security review or a distribution approval.

## Actual import graph

The BPF-006 candidate resolves 1,307 Linux arm64 packages and 1,309 Linux amd64
packages. The only additional import over BPF-005 is the local `oomtrace` package.
The external import graph and all 75/87 non-Go and embedded input hashes are
unchanged. Fresh scans preserve the four advisory IDs below. Both resolve 163
imported modules, including local replacements (552 entries in the full module
graph, which also includes unused dependencies). The retained inventory separately names
75 arm64 and 87 amd64 non-Go/embedded inputs. Assembly, generated embedded objects
and headers need source-level licence/provenance review in addition to the Go
scanner; a module count alone does not close that obligation.

The SDK imports contain embedded BPF objects for file fields, container hooks,
network dispatch, socket enrichment/iteration, traffic-control dispatch and
process iteration. Their presence is recorded even though the candidate does not
select those operators. The complete pinned modified SDK source, embedded inputs
and policy patch are retained in a local source archive. Do not treat a custom
programme allowlist as evidence that these linked dependencies do not exist.

The worker has no imported `golang.org/x/crypto/openpgp` package. Its only imported
containerd CRI subtree package is `github.com/containerd/containerd/pkg/cri/constants`;
it uses containerd v1.7.33 and contains no containerd/v2 package. It exposes no CRI
restore service, runtime socket or checkpoint input. These facts concern the
actual entrypoint graph, not all code available in the module cache.

## Vulnerability findings

`govulncheck` v1.7.0 scanned both Linux architectures against the Go database whose
reported update was 10 September 2026. Both scans reported the same groups below.
Raw findings remain retained, including the containerd called-symbol traces.

| Finding | Actual scan and candidate assessment |
| --- | --- |
| GO-2026-5064 | Module/package/symbol findings for containerd v1.7.33; the primary advisory identifies CDI checkpoint-restore handling in containerd/v2 |
| GO-2026-5338 | Module/package/symbol findings for containerd v1.7.33; the primary advisory identifies checkpoint image tag poisoning in containerd/v2 |
| GO-2026-5622 | Module/package/symbol findings for containerd v1.7.33; the primary advisory identifies checkpoint log symlink restoration in containerd/v2 |
| GO-2026-5932 | Module-only finding for x/crypto; the affected OpenPGP packages are absent from both worker import graphs |

The three primary containerd advisories currently list affected v2.1/v2.2/v2.3
ranges, unlike the broader v1 mapping reported by the Go scanner. The inspected
worker graph excludes the affected restore implementation. Record that discrepancy
and graph evidence for maintainer/independent review; do not suppress the findings
or describe the worker as vulnerability-free. Sources checked on 15 September:
[CDI restore](https://github.com/containerd/containerd/security/advisories/GHSA-33vj-92qq-66hc),
[tag poisoning](https://github.com/containerd/containerd/security/advisories/GHSA-cvxm-645q-p574),
[log restoration](https://github.com/containerd/containerd/security/advisories/GHSA-rgh6-rfwx-v388).

The OpenPGP advisory identifies an unmaintained package with no fixed version.
The worker's signatures use Ed25519 from the standard library. Its other
x/crypto imports do not make OpenPGP part of the linked graph. Keep this absence
as a regression boundary when dependencies change.
[Go advisory](https://pkg.go.dev/vuln/GO-2026-5932).

## Licence and source retention

The arm64 Go licence report, after a documented generated-package resolution,
contains 182 records: 101 Apache-2.0, 39 MIT, 37 BSD-3-Clause, two BSD-2-Clause,
two MPL-2.0 and one ISC. The scanner initially could not identify the worker's
local module licence; its LICENSE and NOTICE now retain the repository's existing
terms for project-owned Go code. They do not relabel bundled third-party code or
the separately licensed custom BPF programmes.

The generated `github.com/in-toto/attestation/go/v1` package has no package-local
licence file. Its v1.2.0 module root identifies Apache-2.0. The resolution record
retains that file's digest, while preserving the scanner's original Unknown row.
Local replacement URL-discovery warnings likewise remain in the raw report.

The optional `licencebundle` build command can now target the worker entrypoint
and retains module licence/notice files in a new output directory. That bundle is
one input to the final inventory: package-specific terms, non-Go/embedded source,
MPL covered-source availability, custom BPF source/build material and the modified
SDK source/patch still need their complete redistribution record. Do not publish
an image from this partial inventory. See [ENGINE_CONTRACT.md](ENGINE_CONTRACT.md)
for the full acceptance and removal obligations.

A separate package-level licence/source copy contains 328 files, including source
selected by the licence copier. The generated in-toto package was excluded only
from automatic copying because its licence detection fails, then added manually
from the verified module licence. It remains in the unfiltered inventory and
findings. The module-root bundle contains 196 files. These are retained local
review artefacts; final reconciliation with non-Go inputs and image contents is
still required.
