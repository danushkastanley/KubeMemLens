# ADR 0027: Migrate the worker SDK containerd dependency

Date: 19 October 2026

Status: implementation prepared for v1.0.1 review

## Context

The worker's upstream SDK test dependencies retain containerd v1. The Go
advisory database maps three checkpoint findings to that module with no fixed
v1 version. The constrained worker does not import the containerd client, but
the repository-wide module findings still block the unchanged release gate.
The latest inspected upstream SDK also retains v1.

## Decision

Extend the existing checksum-bound SDK patch to use containerd v2.3.7. Migrate
the client/test-helper imports and preserve the CRI namespace value `k8s.io`,
whose upstream constant is internal in v2. Update the SDK module requirement
and worker lockfiles. No SDK test is deleted and no finding is suppressed.
The current OSV query reports no findings for v2.3.7; v2.3.2 has newer advisories
and is not the selected patch level.

The constrained worker still excludes container discovery and containerd
clients. Its dependency check now rejects both containerd client generations
and OpenPGP. This updates dependency imports retained outside the worker's
compiled path; it does not re-enable the backends excluded by ADR 0020.

## Alternatives and consequences

Updating CEL and etcd alone leaves four module findings and does not satisfy
the release threshold. Raising the advisory allowance, suppressing containerd
or deleting upstream tests would conceal the unresolved dependency. Updating
the entire SDK would widen the change without removing its v1 requirement.

The v2 module changes some transitive versions and the SDK source patch digest.
The affected SDK packages and their tests must compile; worker dependency and
build checks must pass on both Linux architectures. Static scans do not provide
kernel/runtime qualification. The separate eBPF extension remains development
software and the standard product gains no new support claim.

## Migration and rollback

Update the engine patch digest in the signed contract. A new optional worker
requires a newly built and accepted matching engine/programme/policy bundle;
old acceptance does not authorise the changed SDK. No data migration is needed.
Rollback restores the previous complete matched optional bundle, including its
older dependency findings. Never mix identities or relax admission checks.
