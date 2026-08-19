# Local TUI 2.0 Qualification

Date: 19 August 2026

Outcome: passed

Scope: disposable local kind cluster and real Kubernetes API service-proxy connection

This is sanitised local evidence. It is not a GKE, EKS, AKS, CRI-O, production or high-density support claim.

## Workload profile

- 20 workload Pods in three test namespaces.
- Five controllers across Deployment and StatefulSet kinds.
- Three multi-container Pods, for 23 workload containers in total.
- Anonymous-dominant, filesystem-cache and memory-backed `emptyDir` profiles.
- Restricted Pod security, no service-account token mounting and small explicit requests/limits.

## Terminal verification

The opt-in PTY harness opened the real TUI through the collector service proxy at 80×24, 120×30 and 180×50. The minimum-size run proved:

- risk-ordered Pod rendering and selection beyond the first screen;
- deterministic sort and namespace filtering;
- node-to-Pod navigation and Pod drill-down;
- live selected-Pod history refresh;
- access to the final safe-command section at 80×24;
- pause with advancing age, manual refresh while paused and resume;
- read-only recommendations with automatic mutation disabled; and
- clean exit with terminal restoration.

The 120×30 run repeated rendering, off-screen selection, node navigation and clean exit. The 180×50 run additionally asserted the wide memory dashboard before repeating the same navigation and exit checks. Raw PTY output was not retained because it contains ephemeral workload names.

## Lifecycle and cleanup

The parent kind suite also passed strict doctor, structured queries, explanation evidence windows, history, comparisons, redacted capture/replay, metrics, Helm upgrade, rollback and uninstall. It verified removal of cluster-scoped RBAC before deleting the disposable cluster. A post-run check found no test kind cluster or test node container.

## Repeat

The TUI path is opt-in so the standard local lifecycle remains unchanged:

```sh
E2E_RUN_TUI_SMOKE=true make e2e-kind
```

The harness is [`hack/e2e-tui-kind.sh`](../../hack/e2e-tui-kind.sh), driven by [`hack/tui-smoke.exp`](../../hack/tui-smoke.exp). Its retained JSON summary contains only counts, terminal sizes, check names and pass/fail state.

## Limits

- The run used containerd and cgroup v2 on one local Kubernetes minor.
- It did not establish 5,000/10,000-container performance, provider NetworkPolicy behaviour or managed-provider compatibility.
- Live interruption/recovery remains covered by state-machine and client failure tests rather than this PTY pass.
- No screenshot or raw terminal capture is treated as stronger evidence than the deterministic assertions.
