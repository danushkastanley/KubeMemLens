---
name: verify
description: Run a KubeMemLens change for real and report what was observed. Use before committing or opening a PR, and whenever asked to verify, smoke-test or confirm that a change works. Picks the sample commands or a disposable kind cluster from the diff.
---

# Verify a KubeMemLens change

Goal: observe the changed behaviour on a real path, with the smallest run that can show it. Read `docs/verification.md` for the trigger table; this skill is the how.

## 1. Decide the path from the diff

```sh
git diff --name-only -z main...HEAD > /tmp/kml-changed.bin; git diff --name-only -z >> /tmp/kml-changed.bin
python3 hack/ci_scope.py /tmp/kml-changed.bin
```

- `false`: no kind run. Use the sample path if CLI output changed, otherwise the triggered checks are the verification.
- `true`: the change can reach a running cluster. Use the sample path first if it applies, then the kind path.

## 2. Sample path (no cluster, under a second)

```sh
go run ./cmd/kubectl-memlens sample list
go run ./cmd/kubectl-memlens sample top
go run ./cmd/kubectl-memlens sample explain cache-heavy
```

Samples: `cache-heavy`, `rss-heavy`, `tmpfs-heavy`, `dirty-heavy`, `normal`. Pick the one that exercises the change and read the output. Compare with `main` when the change is to wording, columns or ordering.

## 3. Kind path (disposable cluster, several minutes)

Needs Docker running, plus `kind`, `helm` and `kubectl`. The script builds the image, creates the cluster, writes its kubeconfig to a temporary directory, runs install, diagnosis, upgrade, rollback and uninstall, and deletes the cluster on exit.

```sh
E2E_CLUSTER_NAME=kube-memlens-verify E2E_ARTIFACT_DIR="$(mktemp -d)" hack/e2e-kind.sh
```

Add only the smokes the diff needs:

| Changed area | Add |
| --- | --- |
| Ingestion, collector auth, reliability | `E2E_RUN_AUTHENTICATED_INGESTION_SMOKE=true E2E_RUN_RELIABILITY_SMOKE=true` |
| Tenant reads, RBAC, NetworkPolicy | `E2E_RUN_TENANT_SCOPED_READ_SMOKE=true E2E_RUN_TENANT_ISOLATION_SMOKE=true` |
| TUI, views, key handling | `E2E_RUN_TUI_SMOKE=true` (needs `expect`) |
| Resource metrics source | `E2E_RUN_RESOURCE_METRICS_SMOKE=true` |
| MemoryQoS view | `E2E_RUN_MEMORY_QOS_SMOKE=true` |
| Pod resources | `E2E_RUN_POD_RESOURCE_SMOKE=true` |
| Agentless reader | `E2E_RUN_AGENTLESS_SMOKE=true` |
| CSI volume health | run `CSI_HEALTH_ACKNOWLEDGE=create-and-remove-local-csi-clusters hack/verify-volume-health-kind.sh` instead |

Run it in the background and read the tail of the output when it exits. Leave density benchmarks and the live density smoke off unless the change is about scale.

If the harness has no step that shows the changed behaviour, the run proves only that nothing broke. Say so, and either add the missing assertion to the harness or inspect the behaviour directly before the cluster is removed.

## Rules

- Use a cluster name of your own. Never touch an existing cluster, context or container, including `memlens-demo`, and never a remote cluster.
- Do not set an acknowledgement variable for a script you have not read.
- Do not edit the harness to make a failing run pass. Three failed attempts at the same step: stop and report.
- Confirm afterwards that the cluster is gone: `kind get clusters`.

## Report

State the commands run, the output that shows the behaviour (a few lines, with identifiers redacted), and what was not run and why. Put the same text under "Verification" in the PR description.
