# Contributing to KubeMemLens

Thank you for helping make Kubernetes memory incidents easier to understand.

## Before starting

- Search existing issues and pull requests.
- Open an issue before a large feature, new dependency, public contract, privilege change, or architectural migration.
- Keep changes focused. Separate behaviour-preserving refactors from product changes where practical.
- Review the [architecture](docs/architecture.md), [security model](docs/security-model.md), and [roadmap](docs/roadmap.md) before changing product boundaries.

## Development setup

Requirements:

- Go 1.25 or newer;
- Docker for image and container-based Helm checks;
- kubectl, Helm, and a disposable Kubernetes cluster for end-to-end changes.

Run the local checks:

```sh
make check
```

`make coverage` prints current statement coverage and leaves `coverage.out` for local HTML inspection with `go tool cover -html=coverage.out`. Coverage is evidence of exercised code, not a target that replaces meaningful assertions.

Run the sample paths:

```sh
make run-sample-top
make run-sample-explain
```

For chart work, follow the local-cluster smoke test in [README.md](README.md).

## Verification scope

Match verification to the change. A normal pull request does not require a cloud account, a managed cluster, or a large workload.

- Run focused package tests while developing, then `make check` when the local toolchain and network allow it. CI runs the full repository checks.
- Use kind or minikube for changes to the agent, collector, chart, RBAC, NetworkPolicy, or live TUI behaviour.
- Use synthetic fixtures for parser, aggregation, API-boundary and presentation changes.
- State which checks you ran and which relevant paths you could not run. Missing provider access is acceptable when the change does not make a provider-specific claim.

Maintainers own the GKE, EKS and AKS release matrix. Provider qualification is required before the project publishes a provider-support claim, not before every contribution can be reviewed or merged. Scale testing follows the same rule: a PR needs extra capacity evidence only when it changes the scale contract or makes a new scale claim.

## Pull requests

A pull request should:

- explain the user problem and why the change is needed;
- include behaviour-focused tests for changed behaviour;
- update documentation, fixtures, contracts, and Helm values when relevant;
- state exact verification performed and anything not verified;
- consider security, privacy, compatibility, performance, and terminal accessibility;
- avoid unrelated formatting, refactors, dependency upgrades, or generated files.

KubeMemLens uses UK English for user-facing copy. Diagnoses must describe evidence and uncertainty; do not claim a memory leak or root cause without workload-specific proof.

## Memory and compatibility fixtures

Synthetic, anonymised cgroup fixtures are welcome. Do not submit production file paths, cluster names, namespace names, Pod names, container IDs, logs, credentials, or proprietary workload data. Document the kernel, cgroup mode, Kubernetes version, runtime, and relevant configuration needed to interpret a fixture.

For a false, ambiguous, or unhelpful explanation, use the diagnosis-feedback form and follow the [no-telemetry fixture process](docs/community-feedback.md). Prefer a synthetic fixture or default-redacted incident bundle over production output.

## Dependencies and privileges

New dependencies and privileges require explicit justification. Include maintenance, licence, transitive footprint, security history, runtime cost, least-privilege impact, and removal path. Optional eBPF work requires a separate design, threat model, and benchmark review.

## Conduct and security

Participation is governed by [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md). Report vulnerabilities through [SECURITY.md](SECURITY.md), not a public issue.
