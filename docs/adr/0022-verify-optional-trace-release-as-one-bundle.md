# ADR 0022: Verify the optional trace release as one bundle

Status: Accepted for implementation and local verification; not publication approval.

## Context

The optional extension combines a chart, two-platform image, signed workers and
signed BPF programmes. Verifying each independently can miss a stale worker copy,
wrong image default, substituted SBOM or inconsistent deployment policy. The
standard product has its own release process and must remain capability-free.

## Decision

A bounded canonical manifest is the only release-subject inventory. It binds exact
source/version/contract identities and the chart, engine, image and programme
payloads, with their SBOMs and provenance. Checksums and downloadable asset names
are derived from that manifest rather than maintained separately.

A consumer stages private owned copies, authenticates the manifest against a
caller-selected external trust root and pinned tools, then inspects the payloads
without extracting or executing candidate code. The Go inspector reuses the
existing engine/programme formats and ABI checks; it does not construct runtime
acceptance. Image files must match their separate engine/programme archives, chart
defaults must match the image index, and SBOM file inventories must match actual
payload files. Helm checks use the packaged chart and validate rendered identities,
images, privilege bounds and exact RBAC references.

Local development signatures and GitHub release authority remain distinct. The
read-only CI dry run has no publication or OIDC permissions and transfers expected
manifest/key identities to a separate consumer job. Its results cannot authorise
a beta release. A future protected publication workflow remains subject to the
existing tag, documentation and release-environment gates.

Runtime installation acceptance remains a separate administrator decision over an
immutable policy, exact Node identities and TLS/control pins. Signature verification
does not approve kernel execution, waive resource budgets or qualify a provider.

## Trust boundaries

- Treat downloaded bundles as untrusted data: enforce exact inventory, byte/count
  bounds, canonical paths and no links, ambiguous JSON/YAML or hidden archive data.
- Trust verifier executables and root material only through caller-owned pins;
  bundled public keys cannot select their own authority.
- Authenticate before payload interpretation and offline Helm rendering. Keep tool
  configuration and credentials isolated, and bound execution and file output.
- Reject changed signatures, subjects, contents, platforms, metadata relationships,
  image copies and deployment references. Retain failure evidence.
- A compromised approved publisher or verifier source is outside a checksum/signature
  guarantee. Compiler metadata, SBOM structure and reproduced bytes do not establish
  code safety, complete dependency discovery, licence compliance or a SLSA level.

## Alternatives

Reusing the standard image verifier unchanged would accept the wrong executable
contract and would couple tracing privileges to the ordinary release. It is kept
separate, while the deterministic chart packager and pinned tools are reused.

Executing a verifier carried inside the candidate bundle would bootstrap trust
from the untrusted input. Mutable tags or checksum-only verification would not
establish publisher authority and cross-payload agreement.

## Consequences, migration and rollback

Administrators retain one authenticated set and separately manage installation
policy/TLS prerequisites. Verification is more work than checking one image digest,
but failures identify an incomplete or inconsistent set before installation.

There is no existing published trace bundle format to migrate or beta predecessor
to invent. Local development compatibility evidence remains explicitly local. The
standard release and persisted trace-report contracts are unchanged.

Rollback in the current development window applies the previous verified chart and
matching values through `helm upgrade`, so target preflight runs again. Older
charts lack pre-rollback hooks; native or automatic Helm rollback is not qualified
for that window. Disable and uninstall must verify both Kubernetes resource removal
and captured worker/BPF teardown, preserving the standard installation.

See [build and consumer contract](../ebpf/RELEASE_BUILD.md),
[lifecycle procedure](../ebpf/RELEASE_LIFECYCLE.md) and
[local evidence](../ebpf/RELEASE_LOCAL_VERIFICATION.md).
