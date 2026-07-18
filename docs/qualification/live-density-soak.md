# Live Container-Density and Churn Soak

This runbook exercises KubeMemLens against real Kubernetes containers at the two release-gate densities: 5,000 and 10,000. The harness is intentionally separate from the installation qualification because it consumes material cluster capacity and runs for at least 30 minutes at each density.

The presence of the harness is not performance evidence. Record a result only after a reviewed run completes on an authorised cluster.

## What the harness does

`hack/soak-live-density.sh`:

- requires an exact kubeconfig context, immutable workload image and destructive-action acknowledgement;
- refuses an existing workload namespace and creates a Pod Security `restricted` namespace with an ownership label;
- checks aggregate Linux-node Pod capacity and collector container capacity before creating workloads;
- creates an exact number of sleeping containers across topology-spread Deployment replicas;
- waits for every workload container to be visible through the KubeMemLens paged API;
- samples CLI query latency, cluster mapping, agent scan/post metrics, KubeMemLens Pod health and optional Metrics API resource use;
- runs one zero-surge rolling restart and measures mapping recovery;
- runs final strict `doctor`, writes one sanitised mode-`0600` JSON result and removes only its owned namespace.

It does not install or upgrade KubeMemLens, resize node pools, change RBAC, enable a Metrics Server, publish evidence or infer provider support.

## Capacity planning

The default is ten containers per Pod, so 5,000 containers require 500 Pods and 10,000 require 1,000 Pods. Each container requests `1m` CPU and `1Mi` memory, but its runtime and actual resident memory require additional capacity. The script checks the cluster-wide allocatable Pod count; the scheduler remains the authority for per-node resources, taints, quotas and admission policy.

Use a dedicated test cluster or isolated node pool. Do not run this against a shared production workload merely because the preflight succeeds. For shared multi-tenant test clusters, obtain the cluster owner's approval, apply quotas externally, retain the existing KubeMemLens read RBAC boundary, and inspect the evidence before sharing it.

The workload image must be digest-pinned, support `/bin/sh` and `sleep`, and run correctly as UID `65532` with a read-only root filesystem and no Linux capabilities.

## Gate run

Install and qualify KubeMemLens first, but leave that installation running. Then choose a new evidence directory and workload namespace:

```sh
export SOAK_CONTEXT='<exact-context>'
export SOAK_COLLECTOR_NAMESPACE='kube-memlens'
export SOAK_NAMESPACE='kube-memlens-soak-5000'
export SOAK_WORKLOAD_IMAGE='<trusted-image>@sha256:<64-lowercase-hex-characters>'
export SOAK_ARTIFACT_DIR="$PWD/qualification-evidence/live-5000"
export SOAK_CONTAINERS='5000'
export SOAK_CONTAINERS_PER_POD='10'
export SOAK_DURATION_SECONDS='1800'
export SOAK_ACKNOWLEDGE='run-and-remove-kube-memlens-density-soak'

make soak-live-density
```

Repeat with a fresh namespace and evidence directory for `SOAK_CONTAINERS=10000`. Run each density on every provider/runtime combination for which a density claim will be made. GKE, EKS and AKS are supported targets only on compatible Linux VM node pools; Autopilot-style, Fargate and virtual-node restrictions remain as documented in the main [qualification runbook](../qualification.md).

For a disposable developer check, `SOAK_PROFILE=development` permits 1–500 containers and a 30-second minimum. A development result never satisfies the 5,000/10,000 gate.

## Evidence contract

The only durable output is `density-soak-summary.json`. It deliberately excludes context, cluster, node, namespace, Pod, container and registry names. The immutable image digest remains so reviewers can identify the exact workload artefact.

Each sample records:

- KubeMemLens CLI query duration in milliseconds;
- exact workload container count;
- cluster-wide reported, mapped and unmapped counts;
- aggregated KubeMemLens Pod readiness, restart and OOM-kill counts;
- summed agent found/mapped/unmapped counts, maximum latest scan duration and cumulative post failures;
- separate agent and collector CPU/memory totals, plus their combined total, only when `metrics.k8s.io` is available.

The final record includes steady-state duration and rolling-restart recovery time. Agent post failures are cumulative counters; compare the first and last samples rather than treating a historical non-zero value as a new failure.

## Acceptance review

A release-gate run passes review only when:

1. `outcome` is `passed`, the target is exactly 5,000 or 10,000 and the steady-state duration is at least 1,800 seconds.
2. Every steady-state and post-churn sample reports the exact workload container target.
3. Final strict `doctor` passed, mapping remained complete, and agent post-failure counters did not increase.
4. Every KubeMemLens Pod remained ready, restart and OOM-kill counts did not increase, and no agent scan exceeded its configured scan interval.
5. P95 CLI query latency and churn recovery meet thresholds chosen before the run. Recommended developer-preview targets are at most 2 seconds at 5,000, 5 seconds at 10,000, and 120 seconds for post-rollout mapping recovery; record any justified exception.
6. Metrics API or equivalent Prometheus evidence shows bounded KubeMemLens CPU/memory without throttling or node-pressure impact. If the Metrics API was unavailable, attach separately sanitised monitoring evidence before accepting the run.
7. Cluster events, API-server saturation and a representative non-KubeMemLens canary show no material workload impact. These signals are environment-specific and must be attached separately; the harness does not fabricate them.
8. A human reviews the JSON for identifiers and links it to the exact Kubernetes version, Linux image/kernel, runtime, CNI, chart and KubeMemLens digest in the compatibility record.

The separate monitoring attachment must also report the maximum/delta of agent container filesystem reads and Kubernetes API requests attributed to the KubeMemLens agent ServiceAccount or `kube-memlens-agent/<version>` user agent, split at least by verb and response class. These values are provider/monitoring specific and cannot be derived honestly from cgroup count, so the portable harness deliberately does not fabricate them. Use the definitions in [community diagnosis feedback](../community-feedback.md).

Do not average away a failed node, a post failure, an OOM, a long scan or an incomplete mapping result.

## API scale regression

The collector retains the original unpaginated array endpoints for pre-1.0 compatibility. Current clients use `/api/v1/pages/containers` with at most 500 requested records, automatic byte-aware page reduction, and a hashed opaque keyset continuation token that does not place object names in proxy URLs. Pod, namespace and workload views are deterministically rebuilt from the bounded container pages, so one 10,000-Pod workload cannot create an oversized nested workload response.

`TestContainerPagesServeTenThousandRealisticRecordsWithinResponseBound` stores 10,000 realistic records, confirms the legacy 16 MiB path is bounded, traverses every page without duplicates, and checks each encoded body against the response ceiling. This is API capacity evidence, not a substitute for the live soak.
