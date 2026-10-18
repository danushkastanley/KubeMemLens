# Active qualification on an EKS node host

`eks_campaign.py` runs the existing full paired protocol on the explicitly bound
disposable EKS AL2023 amd64 node. It uses the same normal, high-rate, noisy,
concurrent, bounded-burst and sustained-pressure profiles, observers, replay
functions and thresholds as the local runner. The local entrypoint continues to
require kind. There is no automatic provider selection or fallback.

This entrypoint is preparation tooling. No live EKS execution or provider support
claim follows from unit tests or a single passing workload case.

Follow the [host runtime prerequisites](../ebpf-qualification/EKS_HOST.md),
including approval of the concrete account, region, cost ceiling and cleanup
plan. The outer provider controller must provision, inventory and remove the
cloud resources. This harness does none of those operations and adds no IAM,
SSH or inbound network access.

The private active configuration uses the same fields as the local controller.
Its nested `trace.providerExecution` contains the complete frozen EKS binding
described by the host runtime. The full source hash includes every standard
chart file. `chartInventory` identifies the approved prebuilt native decoder;
the campaign never installs a compiler or compiles during fixture setup. Supply
the matching Linux/amd64 measure, watch, delivery, standard-observer, scheduler
and verifier binaries on the selected host, with every helper digest check
intact. Verifier capture requires the actual kernel BTF and reviewed numeric
probe ABI; local LinuxKit calibration is not evidence for the EKS kernel. Run
the bounded fixed verifier calibration on the exact provider tuple before the
paired campaigns. Source-bound input, probe journals and completed cleanup are
mandatory for every window.

Before measurement, preload the approved immutable standard and fixture images
onto the nodes that need them. Image pull policy remains `Never`, avoiding
registry traffic during paired windows. The standard collector uses
`collector.nodeSelector.kubernetes.io/hostname` to select the verified Node and
serves its extension on Service port 8443. The agent retains its ordinary
DaemonSet placement. Node hostname and UID must match the frozen binding; later
service observations still require the selected Pod, process and cgroup lifetime.
Use the chart version containing the collector selector option.

The EKS runtime reads `/boot/config-<verified-running-kernel>` and the live boot
command line to check memory-cgroup/BPF accounting. Missing configuration, a
changed kernel, disabled accounting or a non-unified hierarchy fails the run.
No kernel settings are changed and no support is inferred from missing files.
The local runtime keeps its bounded `/proc/config.gz` reader.

```sh
python3 hack/ebpf-active-qualification/eks_campaign.py \
  --config /private/approved-active.json \
  --output /private/new-normal-evidence \
  --case normal \
  --acknowledge-owned-eks-host
```

Select exactly one of `normal`, `high-rate`, `noisy`, `concurrent`, `flood` or
`pressure`. There is no reduced-duration option. Each uses five complete pairs.
The whole-run alarm uses the same measurement, setup and restoration allowance
as the certificate lifetime check. A termination or deadline interruption enters
the existing guarded service restoration and fixture cleanup path; the outer
provider controller must still enforce resource lifetime and verify teardown.

The freeze records the runtime's provider-binding and private-kubeconfig hashes,
not account IDs or endpoints. Raw identity/configuration receipts stay private.
Results cover only the selected measured case. Full functional, isolation,
lifecycle, resource, provenance, upgrade/rollback and other product-phase tests
remain separate requirements. Native runtime validation of this entrypoint,
kernel configuration access and collector placement is still required.

## Native observation network boundary

The verified runtime explicitly sets `networkScope: "eks"` in each private
standard-observer and delivery-client configuration. An omitted scope preserves
local-only routing; unknown or null values fail. Both clients use the same
qualification endpoint validator, included in the campaign source freeze.
The EKS route accepts HTTPS port 443 (including its implicit form), bounded DNS
labels and commercial AWS regional `eks.amazonaws.com` or `api.aws` endpoint
forms documented by [AWS](https://docs.aws.amazon.com/eks/latest/userguide/cluster-endpoint.html).
Other partitions and arbitrary hosts are outside this runner's approved scope.

The hostname shape is a routing constraint, not proof of cluster ownership.
Before writing either native configuration, the EKS runtime verifies the exact
API endpoint and CA against the approved provider binding and AWS description.
The native clients retain pinned CA and hostname verification, TLS 1.3, disabled
proxying/redirects, fixed API routes and all existing time/byte limits. An EKS
scope does not authorise provisioning or bypass source, Pod, process or cgroup
identity checks. Configurations and credentials remain private. No live EKS
claim follows from transport unit tests.
