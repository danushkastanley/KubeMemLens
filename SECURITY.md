# Security Policy

## Supported versions

KubeMemLens is in alpha. Alpha versions and the `main` branch receive fixes on a best-effort basis but do not carry a production stability or support guarantee.

The [support and compatibility contract](docs/compatibility.md) is the canonical source for environment, tenant, availability and data-exposure claims. The current alpha is not supported as a shared multi-tenant service.

This table will be updated when the first supported release is published:

| Version | Supported |
|---|---|
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

Until a supported release and response team exist, reports are handled on a best-effort basis. The maintainer will aim to acknowledge a complete report within seven days, coordinate remediation and disclosure with the reporter, and credit the reporter if requested. This is a target, not a contractual service-level agreement.

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
