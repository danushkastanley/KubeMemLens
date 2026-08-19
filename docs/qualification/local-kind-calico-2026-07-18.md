# Local kind and Calico Qualification Evidence

Date: 18 July 2026
Outcome: passed
Purpose: validate the existing-cluster qualification harness and default ingestion NetworkPolicy on an enforcing CNI

This is sanitised local evidence, not a GKE, EKS, AKS, CRI-O, production or high-density support claim.

## Environment

| Component | Evidence |
|---|---|
| Kubernetes | v1.34.8; `kindest/node` pinned to `sha256:02722c2dedddcfc00febf5d27fbeb9b7b2c14294c82109ff4a85d89ac9ba3256` |
| kind | v0.32.0; Darwin arm64 download verified with the upstream checksum file |
| CNI/policy | Calico v3.32.1 manifest; local SHA-256 `a1df919d9721cf667accdc3e72848911b0cb25cfab7d2478ad0c996302c95744` |
| Helm | v3.18.4; Darwin arm64 archive verified with the upstream checksum file |
| Nodes | Two arm64 nodes; Debian 13; LinuxKit kernel 6.12.76; cgroup v2 |
| Runtime | containerd 2.3.1 |
| KubeMemLens image | Temporary local-registry image pinned to `sha256:8715098ca1ee060061bed2766a2a602891bf44675ef76fa86ea4d8aa7b0c4d55` |
| Probe | BusyBox 1.37.0 pinned to `sha256:9532d8c39891ca2ecde4d30d7710e01fb739c87a8b9299685c63704296b16028` |

The temporary KubeMemLens digest is evidence for this build only. Its registry was deleted after the run, so it is not a distributable release artefact.

## Passing checks

- Image status for every KubeMemLens container contained the requested SHA-256 digest.
- The agent DaemonSet desired and ready counts both matched the two schedulable Linux nodes.
- Agent and collector containers were non-privileged, disallowed privilege escalation, used a read-only root filesystem and dropped all capabilities.
- All 11 strict doctor checks passed: build, connection, two fresh node snapshots, cgroup v2, zero cgroup access errors, containerd layout, no Node MemoryPressure, workload context, 15/15 mapped cgroups, store consistency and collector bounds.
- Status, Pod top, machine explanation and collector metrics paths succeeded.
- Machine explanation schema v1 omitted container IDs, cgroup paths, Pod UIDs and labels.
- A same-namespace ordinary Pod reached the read-only port `8080`.
- The same Pod could not reach writable ingestion port `8081`; Calico enforced the chart's NetworkPolicy.
- Helm upgrade to a six-second scan interval succeeded.
- Helm rollback to revision 1 succeeded, and strict doctor recovered after fresh snapshots arrived.
- Helm uninstall removed the release and cluster-scoped KubeMemLens RBAC; the dedicated namespace was deleted.

## Evidence privacy

The generated evidence files were mode `0600`. Connection descriptions and doctor node names were redacted. Review found that the interactive explanation contract also contains a useful Kubernetes node field; the qualification exporter was corrected to verify the raw contract and write `kubernetes.node: redacted` to its shareable bundle. That exact transformation was verified against the successful runtime output.

The temporary JSON, kubeconfig, binaries, Calico manifest and registry data were inspected locally and then moved to Trash. No credentials or raw provider identifiers were retained in the repository.

## Limitations

- kind and LinuxKit do not prove a managed-provider node image, admission controller or provider CNI.
- This run used containerd, not CRI-O.
- It did not create thousands of live containers or measure long-duration CPU, memory or API impact.
- It did not exercise optional eBPF tracing.
- The local image was unsigned and had no release SBOM/provenance; those remain pre-release workflow gates.

The external acceptance procedure remains [the existing-cluster qualification runbook](../qualification.md).
