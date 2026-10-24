# Security Policy

## v1.0.2 security preparation

The source uses Go 1.27.3 across all three modules and pinned builders to
address [GO-2026-6809](https://pkg.go.dev/vuln/GO-2026-6809) in certificate
verification. Trace bundle validation requires the same compiler version.
This change does not replace published binaries or previously signed engine
bundles; deployment requires a separately verified release. The
[v1.0.2 preparation record](docs/release-preparation-1.0.2.md) tracks the fresh
scans, retained advisory and remaining publication gates.

## v1.0.1 security updates

The v1.0.1 source updates Go builders to 1.27.2 and `golang.org/x/net` to
0.60.0 across all three modules for the reviewed HTTP and HTTP/2 advisories.
The optional trace worker also uses `github.com/moby/sys/user` 0.4.1.
CEL 0.30.0 and etcd 3.7.1 close the previously recorded package advisories.
The worker SDK uses containerd v2.3.7 instead of v1, with a new engine patch
identity; previously accepted optional engine bundles do not authorise it.
Existing installed binaries and immutable v1.0.0 artefacts are unchanged;
deploy the new version only after its signed release is published.

See the [release preparation record](docs/release-preparation-1.0.1.md) for
verification and publication status. A passing reachability scan does not
claim an advisory-free dependency graph.

## Historical RC2 baseline

The published `v1.0.0-rc.2` candidate includes gRPC 1.83.2 for the
HTTP/2 fragmented-frame memory-exhaustion and missing-authority denial-of-service advisories. This source change does
not patch the immutable RC1 archives. See [RC2 preparation](docs/release-preparation-rc2.md).

The preparation also updates etcd API/client modules to 3.6.14 and x/crypto to
0.56.0. The preparation record distinguishes patched advisories from remaining
unreachable CEL and unused OpenPGP findings; it does not claim an advisory-free
dependency graph.

## Supported versions

Published standard `1.0.x` releases receive security fixes. Historical prereleases receive fixes on a best-effort basis. The separately packaged eBPF development extension retains its own support status.

The [support and compatibility contract](docs/compatibility.md) is the canonical source for environment, tenant, availability and data-exposure claims. The candidate's shared-cluster claim remains limited to its recorded security and provider evidence.

| Version | Supported |
|---|---|
| Published stable `1.0.x` | Security fixes |
| `1.0.x-rc.N` | Best effort, pre-release |
| `0.0.1-alpha.x` | Best effort, pre-release |
| Unreleased `main` | Best effort |

## Report a vulnerability

Do not open a public issue for a suspected vulnerability.

Use the repository's [private security advisory form](https://github.com/danushkastanley/KubeMemLens/security/advisories/new) when it is available. If GitHub private vulnerability reporting is not enabled, contact the maintainer privately through the contact method on [the maintainer's GitHub profile](https://github.com/danushkastanley).

Include only the information needed to reproduce and assess the issue:

- affected commit or version;
- deployment mode and Kubernetes/runtime versions;
- impact and required attacker access;
- reproduction steps or a minimal proof of concept;
- suggested remediation, if known.

Do not include real cluster credentials, tokens, Pod data, logs containing identifiers, or other sensitive production information. Use synthetic names and redacted evidence.

## Response expectations

Reports are handled on a best-effort basis. The maintainer will aim to acknowledge a complete report within seven days, coordinate remediation and disclosure with the reporter, and credit the reporter if requested. This is a target, not a contractual service-level agreement.

The primary security maintainer owns initial triage. The backup security maintainer
named in [MAINTAINERS.md](MAINTAINERS.md) owns acknowledgement and handover when
the primary is unavailable. A release cannot be promoted to v1 while either role
is vacant.

## Remediation targets

These are operating targets, not guaranteed response times. The clock starts
after a report is confirmed and scoped.

| Severity | Target action |
|---|---|
| Critical or actively exploited | Contain immediately where practical; aim to publish a fix or explicit mitigation within 7 days |
| High | Aim to publish a fix within 14 days |
| Medium | Aim to publish a fix within 30 days; at 60 days require an explicit mitigation, affected-support withdrawal, or documented release block until fixed |
| Low | Address in the next suitable planned release or document why it is accepted |

If maintainer capacity cannot support a safe fix in the target window, the
project will narrow or withdraw the affected support claim and publish an
advisory or mitigation when disclosure is safe. Security fixes use a new
version; published tags, images, charts, archives, and release assets are never
overwritten.

Dependency, secret, vulnerability-reporting, embargo, release and account-
recovery procedures are in the [maintainer security operations runbook](docs/security/maintainer-operations.md).

## Scope

Security-sensitive areas include:

- host cgroup access and the node agent security context;
- Kubernetes RBAC and NetworkPolicy;
- collector ingestion, service-proxy access, and metrics exposure;
- snapshot parsing, resource limits, and denial-of-service resistance;
- container images, Helm packaging, release provenance, and dependencies.

Installation and usage questions belong in [SUPPORT.md](SUPPORT.md). Private
vulnerability reporting is not a private support channel.
