# ADR 0002: Require External Evidence for New Public Claims

Date: 18 July 2026
Amended: 27 August 2026
Status: accepted

## Context

The repository can prepare and locally verify source, chart, image, kind, release, security and documentation paths. It cannot honestly prove provider compatibility, high-density cluster impact, GitHub-hosted provenance, registry publication, Krew acceptance or Artifact Hub listing from a local unpushed worktree. Those actions require external infrastructure, credentials, spend, immutable commits/tags and in some cases third-party review.

## Decision

Treat the following as mandatory external release gates:

- Run the declared GitHub Actions matrix from a clean pushed commit.
- Run a live-container density and churn soak at the largest scale claimed for the release, with node and workload impact measurements. Linux synthetic fixture benchmarks do not support a live-density claim.
- Create and audit a semantic pre-release tag, generated archives, image, OCI chart, checksums, SBOMs, signatures and provenance.
- Publish only after that audit, then submit immutable Krew metadata and list the chart on Artifact Hub.
- Complete the independent eBPF security/performance gates in ADR 0001 before implementing or advertising tracing.

Provider qualification is a separate maintainer-owned claim gate. A new or widened provider/runtime support claim requires one explicitly approved live run and reviewed evidence. The resulting evidence is historical and version-bound: `reviewDueAt` is an advisory freshness date, not a scheduled, CI or per-release failure. Later releases do not provision provider infrastructure merely to refresh a date. A provider-sensitive change narrows the affected claim unless maintainers explicitly approve requalification.

These requirements are not routine pull-request gates. Contributors run checks proportionate to their change and record anything they could not verify. Provider or large-cluster evidence is required from a pull request only when that change makes or widens the corresponding public claim.

External cluster qualification uses `hack/qualify-cluster.sh` with an exact context, new dedicated namespace, immutable workload/probe image digests and explicit acknowledgement. It records sanitised evidence and removes only the release and namespace it created. The script does not create cloud infrastructure, publish results or turn an unrun matrix row into a support claim.

The current list APIs use deterministic ordering, hard store limits, a 16 MiB encoded-response limit and opaque keyset continuation tokens. Current clients page at no more than 500 records and derive nested views client-side. The legacy full-array endpoint remains bounded and fails explicitly at its response ceiling rather than returning a silently incomplete list.

## Alternatives

- Claim provider or live-cluster scale from kind and synthetic fixtures: rejected because it would overstate evidence.
- Recreate every cloud matrix on a schedule or before every release: rejected because it adds recurring credentials, spend and mutation without proving unchanged combinations again. Claims stay bound to the reviewed record and narrow when relevant behaviour changes.
- Publish a tag directly from the local worktree: rejected because it bypasses review and the user's explicit authority over external mutation.
- Silently truncate large responses: rejected because incomplete diagnosis could be mistaken for whole-cluster evidence.
- Add offset pagination: rejected because concurrent node replacement would make offsets unstable. Opaque keyset pagination provides bounded forward traversal without exposing that implementation as a public token contract.

## Consequences

- The source implementation may reach a locally verified release-candidate state while the public release remains blocked.
- `docs/compatibility.md` distinguishes implemented, locally verified, qualification-required, unsupported and deferred rows.
- The public scale ceiling must match the largest reviewed live soak. Higher synthetic results remain development evidence only.
- No “production-ready” or new provider-support claim is permitted before evidence exists. Stale provider evidence produces an advisory warning rather than blocking an unrelated release.
- The local rollback remains deletion of the optional release candidate/cluster. Published artefacts are immutable and require a new version to fix forward.
