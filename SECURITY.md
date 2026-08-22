# Security Policy

## Supported versions

KubeMemLens is in alpha. Alpha versions and the `main` branch receive fixes on a best-effort basis but do not carry a production stability or support guarantee.

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

## Scope

Security-sensitive areas include:

- host cgroup access and the node agent security context;
- Kubernetes RBAC and NetworkPolicy;
- collector ingestion, service-proxy access, and metrics exposure;
- snapshot parsing, resource limits, and denial-of-service resistance;
- container images, Helm packaging, release provenance, and dependencies.
