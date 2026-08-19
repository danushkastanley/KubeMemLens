# Amazon EKS Managed Linux Qualification

Date: 18 July 2026

Outcome: passed for the profile below

Source commit: `1a3002414e2d8477b94f2809470e51c4122fcbf9`

This reviewed record is deliberately stripped of account, registry, cluster, node and network identifiers. It qualifies one profile; it is not a blanket EKS, Fargate, density or production certification.

## Profile

| Area | Observed value |
|---|---|
| Kubernetes | Amazon EKS 1.36.2 |
| Node group | Managed Linux node group, one amd64 node |
| Node OS | Amazon Linux 2023 |
| Runtime | containerd 2.2.4 |
| Cgroup | v2/systemd |
| Network policy | Amazon VPC CNI policy enabled and ingestion denial observed |
| Workload image | Exact SHA-256 digest verified |

## Evidence

- Strict doctor: 11/11 checks passed.
- Mapping: 7/7 container cgroups, with zero read/walk errors.
- Node MemoryPressure and allocatable context were available.
- Top-level workload context was available.
- Read-only collector access succeeded; an unlabelled ingestion probe was denied.
- Explanation privacy, metrics, Helm upgrade, rollback recovery, uninstall, namespace removal and cluster-RBAC removal passed.
- First valid explanation was observed 15 seconds after installation.

## Cleanup

Post-run AWS checks found no disposable EKS cluster, managed node group, active EC2 instance, Auto Scaling group, test launch template, cluster security group, cluster network interface, temporary image repository or dedicated IAM role. Local temporary kubeconfig and downloaded tools were removed.

## Limits

- EKS Fargate is not supported because it cannot run the required DaemonSet.
- The run does not qualify other Kubernetes minors, node operating systems, architectures, runtimes or CNI configurations.
- It is not a 5,000/10,000-container density soak.
- Provider support remains profile-specific and must be revalidated as EKS, Kubernetes and node images advance.
