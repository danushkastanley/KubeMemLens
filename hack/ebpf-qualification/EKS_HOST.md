# EKS node-host idle runner

This is qualification tooling for a disposable, explicitly approved EKS AL2023
amd64 host. It is separate from the kind entrypoint, which still rejects cloud
contexts. No EKS run, provider support or release qualification follows from its
unit tests.

The controller runs on the selected EC2 node host. Observer processes execute
there directly, outside the measured service cgroups. This avoids putting remote
session setup and operator-network round trips into node-side measurements.
`eks_idle_campaign.py` reuses the same five pairs, 900-second windows, 60-second
settles, source binding, thresholds, guarded replica transitions and restoration
as `idle_campaign.py`. Its whole-run alarm is 12,600 seconds. It does not install
a chart, provision cloud resources, transfer credentials or implement teardown of
AWS infrastructure.

Before execution, the provider controller must prepare the approved cloud
ownership inventory and cleanup plan, immutable programme/image/policy and
amd64 observer binaries, installation UIDs/specification hashes, sufficiently
long-lived certificates, private kubeconfig, and all existing provider-network
and capacity prerequisites. Run only after the concrete account/region/cost and
cleanup plan has been approved. The separate canonical provider inventory and
full standard/Node-context/trace protocols remain required.

The runtime configuration extends the existing private idle configuration with
`providerExecution`, whose exact fields are defined in `eks_host_runtime.py`.
Freeze them from fresh AWS and Kubernetes responses before measurement: account,
region, cluster name/creation time/endpoint/CA digest, managed nodegroup, instance,
AMI, instance type, availability zone, boot ID and complete runtime tuple. Do not
invent bindings or substitute a friendly cluster name for its creation lifetime.
The constructor checks ambient account, current EKS cluster and TLS trust, EC2
ownership/image/type/placement, managed-nodegroup membership and the live Node
UID/provider ID/boot/runtime. It requires the local AL2023 amd64 host root,
systemd init, matching boot/kernel and cgroup v2 before making provider requests.
A different host or provider cannot be selected as a fallback.

```sh
python3 hack/ebpf-qualification/eks_idle_campaign.py \
  --config /private/approved-idle.json \
  --output /private/new-idle-evidence \
  --acknowledge-owned-eks-host
```

The input must be an owned regular file with no group/other access; duplicate
JSON fields, symlinks and oversized inputs are rejected. The config contains no
embedded AWS credentials: read-only AWS discovery uses the host's ambient
credential chain. Kubernetes authority must be explicitly scoped and temporary
in the reviewed bootstrap plan. Private configuration and raw identity records
stay outside Git. The evidence manifest records a hash of the provider binding;
it does not copy account, instance, endpoint or credential details into public
numeric observations.

The host controller is privileged qualification infrastructure. Compromise of
its code or credentials can affect its disposable node and whatever Kubernetes
resources its credential permits. The existing UID/spec/resource-version guards
prevent accidental adoption of changed installations; they do not sandbox a
malicious root controller. Keep the frozen controller and inputs private, verify
all supplied executable digests, restrict credential lifetime and scope, and
remove the host and its credentials in the outer cloud teardown. The runtime
itself grants no new IAM/RBAC permissions and opens no inbound ports.

Completed output remains an idle-only result. The frozen environment marks
`sharedKindKernel` false and records the EKS binding hash. Do not use the local
kind public-export path to assert EKS support. Full active/lifecycle/isolation,
other product phases and verified AWS removal are separate completion gates.
The runner still needs execution on the real approved profile; mocked identity
checks and off-host rejection tests are not provider evidence.
