# OpenSSF Best Practices Passing Assessment

Assessment date: 27 August 2026
Criteria: OpenSSF Best Practices Passing
Public project record: <https://www.bestpractices.dev/projects/14259>

This is the evidence map for the public OpenSSF questionnaire. `Met` means the
repository or live project state directly supports the answer. `N/A` is used
only where the criterion permits it. A suggested criterion may remain `Unmet`
with an honest explanation without being converted into an unsupported claim.

## Basics

| Criterion | Status | Evidence or justification |
|---|---|---|
| `description_good` | Met | `README.md` and the GitHub About description state the memory-diagnosis problem in plain language. |
| `interact` | Met | `README.md`, `SUPPORT.md`, `CONTRIBUTING.md`, issue forms, releases, and installation documentation explain how to obtain, use, report, and contribute. |
| `contribution` | Met | `CONTRIBUTING.md` documents the issue and pull-request process. |
| `contribution_requirements` | Met | `CONTRIBUTING.md` defines tests, scope, privacy, dependency, privilege, compatibility, and UK English expectations. |
| `floss_license` | Met | KubeMemLens is released under Apache License 2.0. |
| `floss_license_osi` | Met | Apache-2.0 is OSI approved. |
| `license_location` | Met | `LICENSE` is the byte-for-byte Apache-2.0 text; project attribution is in `NOTICE`. |
| `documentation_basics` | Met | `README.md`, `docs/installation.md`, `docs/security-model.md`, and `docs/compatibility.md` cover installation, use, security, and limitations. |
| `documentation_interface` | Met | `README.md`, command help, `docs/explanation-schema.md`, `docs/metrics.md`, and the aggregated API documentation describe inputs and outputs. |
| `sites_https` | Met | The public repository, releases, packages, and documentation use GitHub HTTPS endpoints. |
| `discussion` | Met | Public GitHub issues and pull requests are searchable, linkable, and open to new participants. |
| `english` | Met | Public documentation, issues, contribution guidance, and product copy use English. |
| `maintained` | Met | Recent reviewed commits, releases, qualification work, dependency triage, and the named primary and backup maintainers demonstrate active maintenance. |

## Change control

| Criterion | Status | Evidence or justification |
|---|---|---|
| `repo_public` | Met | `https://github.com/danushkastanley/KubeMemLens` is public. |
| `repo_track` | Met | Git records author, commit, timestamp, and exact changes. |
| `repo_interim` | Met | Feature branches and pull requests expose interim versions for review. Private security fixes may use GitHub's advisory fork. |
| `repo_distributed` | Met | The repository uses Git. |
| `version_unique` | Met | Release tags and embedded build identities uniquely identify each release. |
| `version_semver` | Met | Release versions follow SemVer, including pre-release identifiers. |
| `version_tags` | Unmet | Legacy GitHub release records `v0.0.1-rc.1` and `v0.0.1-rc.2` have no corresponding Git refs. The current release gate requires an annotated, protected tag before publication, but the historical inconsistency remains visible. |
| `release_notes` | Met | `CHANGELOG.md` and GitHub Releases contain human-readable upgrade-impact summaries. |
| `release_notes_vulns` | N/A | No published KubeMemLens release has fixed a publicly known project vulnerability with a CVE or equivalent identifier. The release runbook requires such identifiers in future notes. |

## Reporting

| Criterion | Status | Evidence or justification |
|---|---|---|
| `report_process` | Met | `SUPPORT.md` and the bug form provide the public report process. |
| `report_tracker` | Met | GitHub Issues tracks individual public reports. |
| `report_responses` | Met | The applicable issue history contains no unacknowledged human bug majority; maintainers own ongoing acknowledgement. |
| `enhancement_responses` | Met | The applicable issue history contains no unanswered human enhancement majority; proposals are routed through public issues and review. |
| `report_archive` | Met | GitHub Issues and pull requests retain a publicly searchable URL-addressed archive. |
| `vulnerability_report_process` | Met | `SECURITY.md` publishes the vulnerability-reporting process. |
| `vulnerability_report_private` | Met | GitHub private vulnerability reporting is enabled and linked directly from `SECURITY.md` and the issue chooser. |
| `vulnerability_report_response` | N/A | No real vulnerability was reported in the previous six months. A synthetic private-reporting drill was acknowledged and closed in seconds without publication. |

## Quality

| Criterion | Status | Evidence or justification |
|---|---|---|
| `build` | Met | Go, Make, Docker, Helm, and GoReleaser rebuild binaries, images, charts, and release archives from source. |
| `build_common_tools` | Met | The build uses common Go, Make, Docker, Helm, and GitHub Actions tooling. |
| `build_floss_tools` | Met | The required local and hosted build tools are FLOSS. |
| `test` | Met | The repository publishes Go, Python, shell, chart, security, lifecycle, scale, provider, terminal, and consumer verification suites. |
| `test_invocation` | Met | `make check` is the documented standard full-suite command. |
| `test_most` | Unmet | Current aggregate statement coverage is 64.2%. Risk-focused integration and negative tests are extensive, but the project does not claim most branches are covered. |
| `test_continuous_integration` | Met | Pull requests and `main` run public required CI across Go, Helm, supply-chain, CodeQL, and supported Kubernetes minors. |
| `test_policy` | Met | `CONTRIBUTING.md` requires behaviour-focused tests for changed behaviour and important failure paths. |
| `tests_are_added` | Met | Recent authentication, authorisation, isolation, reliability, scale, provider, terminal, and release changes include corresponding automated tests. |
| `tests_documented_added` | Met | `CONTRIBUTING.md` and the pull-request template make changed-behaviour tests explicit. |
| `warnings` | Met | `go vet`, formatting checks, ShellCheck, actionlint, strict Helm lint, kubeconform, and GoReleaser checks run in the gate. |
| `warnings_fixed` | Met | The complete gate passes without accepted compiler, vet, shell, workflow, or chart warnings. |
| `warnings_strict` | Met | Helm lint and Kubernetes schema validation are strict; high/critical configuration and image findings fail CI. |

## Security

| Criterion | Status | Evidence or justification |
|---|---|---|
| `know_secure_design` | Met | On 27 August 2026, the primary maintainer attested to secure-software design knowledge. The threat model, security ADRs, least-privilege chart, delegated authorisation, replay controls, and adversarial tests provide applied evidence. |
| `know_common_errors` | Met | On 27 August 2026, the primary maintainer attested to knowledge of common Kubernetes and Go vulnerability classes and mitigations. Boundary validation, authentication, resource authorisation, injection-safe transport, request bounds, secret handling, and denial-of-service controls provide applied evidence. |
| `crypto_published` | Met | The project uses standard Go and Kubernetes TLS, X.509, ECDSA P-256, and SHA-256 implementations rather than a private protocol. |
| `crypto_call` | Met | Cryptographic work calls Go standard-library and Kubernetes security components; KubeMemLens does not reimplement primitives. |
| `crypto_floss` | Met | Go and Kubernetes cryptographic implementations used by the project are FLOSS. |
| `crypto_keylength` | Met | Serving CA and leaf keys use ECDSA P-256; the TLS client requires TLS 1.2 or newer. |
| `crypto_working` | Met | Production security mechanisms do not depend on MD4, MD5, DES, RC4, Dual EC, ECB, or another broken default. |
| `crypto_weaknesses` | Met | Production security mechanisms do not depend on SHA-1 or a known weak default mode. |
| `crypto_pfs` | Met | The supported Go/Kubernetes TLS path negotiates modern ephemeral key exchange; KubeMemLens does not configure a static key-exchange suite. |
| `crypto_password_storage` | N/A | KubeMemLens does not accept or store passwords for external-user authentication. Kubernetes owns caller authentication. |
| `crypto_random` | Met | Certificate keys and serial numbers use `crypto/rand.Reader`. |
| `delivery_mitm` | Met | Source, releases, OCI images, and charts use HTTPS; Git may use SSH. Release subjects also have checksums, signatures, and attestations. |
| `delivery_unsigned` | Met | Release checksums are retrieved over HTTPS and verified through signed release subjects; no HTTP-downloaded hash is trusted. |
| `vulnerabilities_fixed_60_days` | Met | No unpatched medium-or-higher KubeMemLens project vulnerability has been publicly known for more than 60 days. Dependency advisories are tracked separately by reachability and patch policy. |
| `vulnerabilities_critical_fixed` | Met | No critical KubeMemLens project vulnerability is open; `SECURITY.md` defines immediate containment and a seven-day fix-or-mitigation target. |
| `no_leaked_credentials` | Met | GitHub secret scanning and push protection are enabled, Trivy scans committed content, and live readback found zero open secret alerts. |

## Analysis

| Criterion | Status | Evidence or justification |
|---|---|---|
| `static_analysis` | Met after PROD-011 merge | CodeQL, `govulncheck`, Trivy, and `go vet` cover the proposed production release; CodeQL publishes SARIF on pull requests and `main`. |
| `static_analysis_common_vulnerabilities` | Met after PROD-011 merge | CodeQL, `govulncheck`, and Trivy include vulnerability-focused rules and advisory data. |
| `static_analysis_fixed` | N/A | No confirmed exploitable medium-or-higher KubeMemLens vulnerability has been found by static analysis. The maintainer runbook makes confirmed findings release blockers until fixed or explicitly contained. |
| `static_analysis_often` | Met after PROD-011 merge | CodeQL runs on every pull request and `main` update; other static checks run in required CI. |
| `dynamic_analysis` | Unmet | The test suite executes real behaviour, race detection, lifecycle, and adversarial paths, but it does not claim 80% branch coverage or continuous fuzzing. |
| `dynamic_analysis_unsafe` | N/A | KubeMemLens is implemented in Go and does not produce C or C++ project code. |
| `dynamic_analysis_enable_assertions` | Met | Unit, integration, negative, race, Helm, and lifecycle tests execute with their full assertions enabled. |
| `dynamic_analysis_fixed` | N/A | No exploitable medium-or-higher project vulnerability has been found through dynamic analysis. |

## Remaining Passing actions

1. Merge PROD-011 so CodeQL and the published Scorecard execute on `main`.
2. Complete public project 14259 from these evidence-backed answers without
   upgrading `Unmet` or `N/A` entries for appearance.
3. Record the achieved level, published Scorecard result, and any
   closure work in `openssf-baseline.md`.
