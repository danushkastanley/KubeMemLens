# Optional trace development chart

This separate chart packages the trace API and node processes. It is disabled by
default and creates no resources until explicitly enabled. It is not a dependency
of the standard KubeMemLens chart.

**No resource-qualified profile is available.** The only selectable profile is
`development-linux-containerd`, with `acknowledgeUnqualifiedDevelopment: true`.
Successful installation establishes functional preconditions, not performance,
provider support, independent review or release qualification.

See the [installation and removal runbook](../../docs/ebpf/CHART_INSTALLATION.md)
before enabling this chart. Administrators supply the accepted image digest,
installation policy, TLS material, exact node profiles and preflight account.
Private keys must not be put in Helm values.

Follow the [operator walkthrough](../../docs/ebpf/OPERATIONS.md) for the complete
install-to-removal workflow and [trace support](../../docs/ebpf/SUPPORT.md) before
sharing diagnostics. [Incident playbooks](../../docs/ebpf/INCIDENTS.md) cover
overhead, uncertain cleanup, missing evidence, outages and isolation failures.

Validate the disabled default:

```sh
helm lint --strict charts/kube-memlens-trace
helm template trace charts/kube-memlens-trace
```

The second command produces no Kubernetes resources. The repository's
`make check-trace-chart-contract` verifies enabled manifests, rejected inputs,
separate permissions and the unchanged standard chart.
