# Community Diagnosis Feedback

KubeMemLens does not phone home or collect product analytics. Product usefulness and diagnosis accuracy are measured only from local qualification evidence and information that contributors deliberately submit in public issues or pull requests.

## Privacy boundary

- Prefer synthetic cgroup fixtures or default-redacted incident bundles.
- Never request kubeconfig data, credentials, customer/workload names, raw Kubernetes objects, Pod UIDs, container IDs, labels, image names, or sensitive paths.
- Treat an explicitly disclosed raw trace as sensitive even when the reporter chose to include paths; move security material to the private process in `SECURITY.md`.
- Do not copy a public report into the repository as a fixture without the contributor's agreement and a second maintainer privacy review.

The [diagnosis feedback issue form](../.github/ISSUE_TEMPLATE/diagnosis_feedback.yml) requests the smallest versioned finding, independent ground truth, and a reproducible redacted fixture.

## Maintainer review

For each accepted report, record one outcome in the issue:

1. confirmed false diagnosis;
2. confirmed ambiguous or missing-caveat diagnosis;
3. expected behaviour with clearer documentation needed;
4. not reproducible from the supplied evidence; or
5. security-sensitive and moved to private reporting.

A behaviour change requires a synthetic regression fixture and test. Do not weaken confidence or caveat assertions merely to make a disputed example pass.

## Release measures

Review these measures before promoting a public release:

| Measure | Evidence source |
|---|---|
| Install to first valid explanation | `qualification-summary.json.measurements.installToFirstValidExplanationSeconds` |
| Mapping coverage and layout support | `doctor.json`, compatibility rows, and synthetic cgroup/runtime fixtures |
| False or ambiguous diagnoses | Opt-in diagnosis-feedback issues and accepted synthetic regression fixtures |
| Agent CPU and memory | Per-component samples in the live-density record |
| Agent cgroup filesystem reads | A separately sanitised cAdvisor/Prometheus delta for the agent containers; the harness does not infer syscalls from container count |
| Agent Kubernetes API calls | A separately sanitised API-server metric or audit aggregate for the agent ServiceAccount or `kube-memlens-agent/<version>` user agent, split by verb and response class |
| Collector memory and response latency | Per-component density samples, CLI query latency, collector request metrics, and churn recovery |
| Explanations with recent deltas and a next step | Review the versioned findings in the agreed fixture set; numerator requires `counterDeltaKnown=true`, direct pressure/event evidence, and at least one suggested check |
| Reproducible distribution | GitHub-hosted CI, kind install tests, release checksums/SBOM/provenance, immutable image/chart identities, and Krew/Helm install evidence |

Always publish the denominator, fixture-set revision, cluster/runtime profile, and measurement window beside a percentage or maximum. A development smoke, synthetic benchmark, or unrun harness is not a release-gate result.
