# ADR 0002: Require External Qualification Before Public Release

Date: 18 July 2026
Status: accepted

## Context

The repository can prepare and locally verify source, chart, image, kind, release, security and documentation paths. It cannot honestly prove provider compatibility, high-density cluster impact, GitHub-hosted provenance, registry publication, Krew acceptance or Artifact Hub listing from a local unpushed worktree. Those actions require external infrastructure, credentials, spend, immutable commits/tags and in some cases third-party review.

## Decision

Treat the following as mandatory external release gates, not as inferred support:

- Run the declared GitHub Actions matrix from a clean pushed commit.
- Qualify representative GKE, EKS and AKS Linux node pools and a NetworkPolicy-enforcing CNI.
- Qualify CRI-O if it remains in the public compatibility contract.
- Run 5,000/10,000 live-container density and churn soaks with node/workload impact measurements. Linux synthetic fixture benchmarks are useful development evidence but do not satisfy this gate.
- Create and audit a semantic pre-release tag, generated archives, image, OCI chart, checksums, SBOMs, signatures and provenance.
- Publish only after that audit, then submit immutable Krew metadata and list the chart on Artifact Hub.
- Complete the independent eBPF security/performance gates in ADR 0001 before implementing or advertising tracing.

External cluster qualification uses `hack/qualify-cluster.sh` with an exact context, new dedicated namespace, immutable workload/probe image digests and explicit acknowledgement. It records sanitised evidence and removes only the release and namespace it created. The script does not create cloud infrastructure, publish results or turn an unrun matrix row into a support claim.

The current list APIs use deterministic ordering, hard store limits, a 16 MiB encoded-response limit and opaque keyset continuation tokens. Current clients page at no more than 500 records and derive nested views client-side. The legacy full-array endpoint remains bounded and fails explicitly at its response ceiling rather than returning a silently incomplete list.

## Alternatives

- Claim provider or large-cluster support from kind and synthetic fixtures: rejected because it would overstate evidence.
- Publish a tag directly from the local worktree: rejected because it bypasses review and the user's explicit authority over external mutation.
- Silently truncate large responses: rejected because incomplete diagnosis could be mistaken for whole-cluster evidence.
- Add offset pagination: rejected because concurrent node replacement would make offsets unstable. Opaque keyset pagination provides bounded forward traversal without exposing that implementation as a public token contract.

## Consequences

- The source implementation may reach a locally verified release-candidate state while the public release remains blocked.
- `docs/compatibility.md` distinguishes prepared, locally verified and externally qualified rows.
- Any final hand-off must list these gates plainly; no “production-ready” or provider-support claim is permitted before evidence exists.
- The local rollback remains deletion of the optional release candidate/cluster. Published artefacts are immutable and require a new version to fix forward.
