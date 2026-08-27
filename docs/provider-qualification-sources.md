# PROD-008 provider and runtime qualification sources

Review date: 2026-08-26

This note records primary-source facts used to design PROD-008 qualification and interpret its one-time results. A support row requires a passing, reviewed result for the exact image, chart and environment linked from the support contract. Historical evidence is not a claim about unrecorded provider versions.

## Cross-provider evidence

- Record the control-plane Kubernetes version from the provider API or `kubectl version`. Record the node Kubernetes version, operating-system image, architecture, kernel, kubelet and container runtime from live Node status. Kubernetes defines these as Node status fields; a provider image-family label alone is not runtime proof. [Kubernetes Node status](https://kubernetes.io/docs/reference/node/node-status/)
- Keep the agent's `kubernetes.io/os: linux` selector. In a mixed Linux and Windows cluster, `.spec.os.name` does not drive scheduling; Kubernetes recommends OS-specific node selectors or equivalent taints and tolerations. A DaemonSet creates Pods only for nodes eligible under its selector or affinity. [Kubernetes Windows scheduling](https://kubernetes.io/docs/concepts/windows/user-guide/#ensuring-os-specific-workloads-land-on-the-appropriate-container-host), [Kubernetes DaemonSet scheduling](https://kubernetes.io/docs/concepts/workloads/controllers/daemonset/#running-pods-on-select-nodes)
- Do not treat an accepted `NetworkPolicy` object as enforcement evidence. Kubernetes states that the cluster must use a network plugin that implements NetworkPolicy; otherwise the API can exist while policies have no effect. Qualification must exercise an allowed path and a denied path. [Kubernetes NetworkPolicy](https://kubernetes.io/docs/concepts/services-networking/network-policies/), [Kubernetes networking model](https://kubernetes.io/docs/concepts/services-networking/)
- Before cluster deletion, remove and observe cleanup of any qualification-created `LoadBalancer` Service. Kubernetes attaches a cleanup finalizer to these Services, but provider guidance still identifies provider-specific retained or orphanable resources. Handle Ingress cleanup through the provider or installed Ingress controller's documented path. [Kubernetes load-balancer cleanup](https://kubernetes.io/docs/tasks/access-application-cluster/create-external-load-balancer/#garbage-collecting-load-balancers)

Retain only sanitised grouped values in a published bundle. Account, subscription, project, cluster, node, VPC, subnet, resource-group, instance, registry and private-address identifiers belong only in the operator's private cleanup record.

## GKE Standard

### Profile facts and constraints

- GKE Standard offers Container-Optimized OS with containerd (`cos_containerd`) and Ubuntu with containerd (`ubuntu_containerd`). Standard lets the operator select a node image; Autopilot always uses `cos_containerd`. COS is minimal and does not provide conventional package management, while Ubuntu uses the standard Linux filesystem layout. These are distinct qualification rows. [GKE node images](https://cloud.google.com/kubernetes-engine/docs/concepts/node-images), [specify a GKE node image](https://cloud.google.com/kubernetes-engine/docs/how-to/node-images)
- For Calico-backed GKE NetworkPolicy, both the add-on and node enforcement must be enabled. GKE Dataplane V2 is a separate datapath whose NetworkPolicy behaviour must be recorded rather than inferred as Calico. [GKE NetworkPolicy enforcement](https://cloud.google.com/kubernetes-engine/docs/how-to/network-policy), [GKE Dataplane V2](https://cloud.google.com/kubernetes-engine/docs/how-to/dataplane-v2)
- GKE Autopilot permits read-only `hostPath` only under `/var/log` and blocks other host paths. It therefore cannot expose KubeMemLens's required read-only `/sys/fs/cgroup` mount. [GKE Autopilot security](https://cloud.google.com/kubernetes-engine/docs/concepts/autopilot-security#built-in-security-settings)

### Exact inventory to retain

Use `gcloud container clusters describe` and `gcloud container node-pools describe`, scoped with the exact project and location, to record the control-plane version, node-pool version, `config.imageType`, locations, architecture or machine type, status, NetworkPolicy configuration and datapath provider. The GKE NodePool API defines the pool version and configuration, and the cluster API defines NetworkPolicy and datapath fields. Correlate those values with grouped live Node status for the exact OS image, kernel and `containerRuntimeVersion`. [GKE node-pool CLI](https://cloud.google.com/sdk/gcloud/reference/container/node-pools/describe), [GKE NodePool resource](https://cloud.google.com/kubernetes-engine/docs/reference/rest/v1/projects.locations.clusters.nodePools), [GKE Cluster resource](https://cloud.google.com/kubernetes-engine/docs/reference/rest/v1/projects.locations.clusters)

### Cleanup verification signals

After authorised deletion, a read-only cluster describe or list must no longer return the qualification cluster, and no qualification node pool or node instance may remain. Also check the exact qualification labels or private resource inventory for load balancers and persistent disks: GKE attempts to remove load balancers but recommends deleting `LoadBalancer` Services first, and it retains persistent disks during cluster deletion. [Delete a GKE cluster](https://cloud.google.com/kubernetes-engine/docs/how-to/deleting-a-cluster)

## Amazon EKS managed nodes

### Profile facts and constraints

- The narrow row is an amd64 managed node group using the standard EKS-optimised AL2023 AMI (`AL2023_x86_64_STANDARD`), not a custom, accelerated or Bottlerocket image. EKS-optimised Amazon Linux AMIs include `kubelet`, the AWS IAM Authenticator and containerd; new managed node groups on Kubernetes 1.30 and later default to AL2023. [EKS-optimised Amazon Linux AMIs](https://docs.aws.amazon.com/eks/latest/userguide/eks-optimized-ami.html), [EKS Nodegroup API](https://docs.aws.amazon.com/eks/latest/APIReference/API_Nodegroup.html)
- Managed node groups provision EC2 instances in an EKS-managed Auto Scaling group and drain nodes during managed updates and termination. Qualification should prove the group type rather than infer it from an `aws://` Node provider ID. [EKS managed node groups](https://docs.aws.amazon.com/eks/latest/userguide/managed-node-groups.html)
- Amazon VPC CNI NetworkPolicy is not enabled by default. Record the add-on version and configuration, and exercise enforcement; current AWS guidance also limits its policy enforcement to EC2 Linux nodes, excluding Fargate and Windows nodes. [Configure EKS VPC CNI NetworkPolicy](https://docs.aws.amazon.com/eks/latest/userguide/cni-network-policy-configure.html), [EKS NetworkPolicy considerations](https://docs.aws.amazon.com/eks/latest/userguide/cni-network-policy.html)
- Fargate does not provide a node DaemonSet execution model for the KubeMemLens agent. AWS instructs workloads that require a daemon to use a sidecar and notes that node DaemonSets run only on EC2 instances. [EKS on Fargate](https://docs.aws.amazon.com/eks/latest/userguide/fargate.html)

### Exact inventory to retain

Use `aws eks describe-cluster` and `aws eks describe-nodegroup` in the exact account and Region. Record the cluster version/platform, node-group `amiType`, `releaseVersion`, Kubernetes version, capacity type, launch-template use, status and associated Auto Scaling group, plus the VPC CNI add-on version and NetworkPolicy configuration. `DescribeNodegroup` is the provider-owned proof that the pool is managed and identifies the deployed EKS-optimised AMI release. Correlate it with grouped live Node status for AL2023 build, amd64, kernel, kubelet and containerd versions. [EKS DescribeNodegroup](https://docs.aws.amazon.com/eks/latest/APIReference/API_DescribeNodegroup.html), [AWS CLI `describe-nodegroup`](https://docs.aws.amazon.com/cli/latest/reference/eks/describe-nodegroup.html)

### Cleanup verification signals

Delete qualification-created external Services and Ingresses before cluster deletion so their load balancers can be released. The private cleanup record should then show no managed node groups or Fargate profiles, `DescribeCluster` returning not found, and no qualification-owned Auto Scaling group, EC2 instance, CloudFormation stack, load balancer, ECR repository or managed Prometheus scraper. Check only resources created for the run; shared VPC and IAM resources are not cleanup targets. [Delete an EKS cluster](https://docs.aws.amazon.com/eks/latest/userguide/delete-cluster.html), [delete an EKS managed node group](https://docs.aws.amazon.com/eks/latest/userguide/delete-managed-node-group.html)

## Azure Kubernetes Service

### Profile facts and constraints

- The narrow row is an amd64 Linux node pool whose recorded OS SKU and node image resolve to Ubuntu with containerd. Ubuntu is the default Linux node image, but the exact Ubuntu release can change with Kubernetes version and regional image rollout; for example, the unversioned `Ubuntu` SKU moves to Ubuntu 24.04 with Kubernetes 1.35 or later. Record `nodeImageVersion` rather than publishing a floating `Ubuntu` claim. [AKS node images](https://learn.microsoft.com/en-us/azure/aks/node-images)
- AKS Linux node pools on Kubernetes 1.19 and later use containerd. Live Node status must still record the exact runtime version. [AKS security concepts](https://learn.microsoft.com/en-us/azure/aks/concepts-security#node-security)
- The provider inventory accepts only VM scale-set pools. Current Azure CLI
  output reports `typePropertiesType=VirtualMachineScaleSets` alongside the
  resource envelope's `type`; the exact legacy pool `type` remains accepted
  only when `typePropertiesType` is absent.
- Record the actual NetworkPolicy engine. AKS documents different platform constraints for Cilium, Azure NPM and Calico, and records retirement dates for Azure NPM. Merely creating a NetworkPolicy does not prove enforcement. [AKS NetworkPolicy options](https://learn.microsoft.com/en-us/azure/aks/use-network-policies)
- AKS configures aggregated APIs with a request-header client CA and an empty
  `--requestheader-allowed-names` value. The recorded KubeMemLens candidate
  deliberately requires a named proxy client and therefore failed closed on
  standard AKS rather than widening forwarded-header trust. This is a
  candidate-specific authentication incompatibility, not evidence that AKS can
  never be supported. [AKS Engine aggregated API configuration](https://github.com/Azure/aks-engine-azurestack/blob/master/docs/topics/clusterdefinitions.md)
- AKS virtual nodes run Pods on Azure Container Instances. DaemonSets do not deploy to virtual nodes, and virtual-node networking has NetworkPolicy limitations, so they cannot satisfy the deep agent row. [AKS virtual nodes](https://learn.microsoft.com/en-us/azure/aks/virtual-nodes#limitations)
- Azure virtual-kubelet Nodes may leave OS image, container runtime and kernel
  fields blank. Qualification records those facts as `unreported`, retains the
  reported kubelet version, uses only the standard architecture label for
  amd64 evidence, and records architecture as `unreported` when that label is
  also absent.

### Exact inventory to retain

Use `az aks show` and `az aks nodepool show` for the exact subscription, resource group, cluster and pool. Record `currentKubernetesVersion`, `networkProfile`, node resource group, pool `osType`, `osSku`, `nodeImageVersion`, `currentOrchestratorVersion`, architecture or VM size, mode, taints and status. Correlate these provider fields with grouped live Node status for Ubuntu build, amd64, kernel, kubelet and containerd versions. [AKS managed-cluster resource](https://learn.microsoft.com/en-us/rest/api/aks/managed-clusters/get), [AKS agent-pool resource](https://learn.microsoft.com/en-us/rest/api/aks/agent-pools/get), [Azure CLI `az aks nodepool`](https://learn.microsoft.com/en-us/cli/azure/aks/nodepool)

### Cleanup verification signals

After authorised deletion, `az aks show` must no longer return the qualification cluster and the recorded node resource group must be absent. AKS documents that cluster deletion removes the control plane, node resource group, VM scale sets or VMs and cluster-owned networking and storage in that group. Separately verify any qualification-owned resource outside the node resource group; do not delete a shared VNet, subnet, identity, registry or monitoring resource. [Delete an AKS cluster](https://learn.microsoft.com/en-us/azure/aks/delete-cluster)

## Real CRI-O reference profile

Use an amd64 Ubuntu 24.04 LTS kubeadm cluster as the first practical self-managed CRI-O reference profile. This is a project qualification target, not a CRI-O or KubeMemLens support claim. The CRI-O project publishes generic deb packages and stable package streams for current Kubernetes minors. Its separate source-build guide documents dependencies for Ubuntu 24.04 or later. CRI-O follows Kubernetes minor release lines, so record matching Kubernetes and CRI-O minors and their exact patch versions. [CRI-O package streams](https://github.com/cri-o/packaging/blob/main/README.md), [CRI-O source installation](https://github.com/cri-o/cri-o/blob/main/install.md), [CRI-O compatibility matrix](https://github.com/cri-o/cri-o#compatibility-matrix-cri-o--kubernetes)

Retain the Ubuntu release, kernel, architecture, cgroup filesystem and mode, kubelet version, `crio --version`, `crictl info`, configured runtime endpoint, OCI runtime, CNI implementation and NetworkPolicy enforcement result. Grouped Kubernetes Node status must report `cri-o://<exact-version>`. Cleanup is owned by the self-managed infrastructure plan: record the exact dedicated machines and network resources before the run, remove only those authorised resources, then prove they and the temporary registry artefacts are absent without publishing hostnames, addresses or account identifiers.

## Qualification decision boundary

Provider inventory proves what was provisioned; live Node status proves what ran; the KubeMemLens lifecycle and adversarial probes prove behaviour. All three are required. A provider-family prefix, Helm render, accepted NetworkPolicy object, synthetic cgroup fixture or old passing bundle is insufficient to widen `docs/compatibility.md`.
