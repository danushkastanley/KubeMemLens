# Scale qualification

This gate tests one declared KubeMemLens build against one reviewed workload profile. A passing result supports only that exact profile and environment. Configured store limits, synthetic benchmarks and the development smoke are not live-scale claims.

The gate mutates a disposable cluster. It creates a workload namespace, temporarily stops and restores the agent DaemonSet, replaces an exact workload Pod batch, and runs the reliability failure sequence for the qualification profile. Use a cluster whose owner has authorised those actions.

## Profiles

Profiles live under `hack/scale-profiles`. Their canonical SHA-256 digest covers every field except `profileDigest`. The runner rejects legacy environment overrides, so container count, duration, image and budgets cannot change after review.

| Profile | Purpose | Workload | Evidence |
| --- | --- | --- | --- |
| `development-smoke` | Ordinary kind regression smoke. It makes no scale claim. | 20 containers, 10 per Pod, 30 seconds, sampled every 5 seconds, 5-second agent interval, 32 MiB canary transfer. | At least 7 steady-state samples, 7 control samples and exact replacement of 2 Pods at full density. Optional telemetry is reported as `not_evaluated` when unavailable. |
| `rc-5000` | Local release-candidate qualification. | 5,000 containers, 50 per Pod, 30 minutes, sampled every 30 seconds, 5-second agent interval, 256 MiB canary transfer. | At least 61 steady-state samples, 7 control samples and exact replacement of 10 Pods at full density. Every telemetry source is required. |

`rc-5000` has no accepted result yet. It remains a declared test profile, not a supported capacity. The [support contract](compatibility.md) stays at `Qualification required` until a passing run and its environment record receive review.

Both profiles use the digest-pinned BusyBox image recorded in the profile. The density containers request 1 MiB of memory and 1 millicore of CPU. They sleep, so they test collection density rather than application throughput. The separate canary provides the control and observed workload comparison.

The runner creates the development workload in one two-Pod batch. The RC workload scales in ten-Pod batches until all 100 Pods are available. The same profile field sets the later replacement batch. This bounds both container creation and measured churn without changing the 5,000-container steady state or its budgets.

## Predeclared budgets

The RC profile uses these release-claim budgets. The development smoke uses the same bounds except for a 10% canary regression limit, which reduces noise in the short 32 MiB probe and does not support a scale claim.

| Check | Budget | Evaluator rule |
| --- | --- | --- |
| Mapping and node coverage | 100% | Every sample must contain the exact workload container count, zero unmapped containers, `ready` reliability, and equal expected and fresh Node counts with no stale or missing Nodes. |
| Operational stability | No unexplained restart or OOM kill | Every sample compares KubeMemLens Pod status with the pre-run baseline. |
| Agent scan p99 | Below 4,000 ms | Exclusive bound. This is 80% of the default 5-second agent interval. A different interval is not this profile. |
| CLI p95 | At or below 2,000 ms | Time for `doctor --output json`. |
| TUI p95 | At or below 2,000 ms | Time to start the 80 by 24 TUI and render the initial Pod table from the collector response. |
| Recovery | At or below 120 seconds per event | Exact workload batch replacement, agent, collector, Node, Kubernetes API and partial-rollout recovery are checked separately. The workload timer covers Pod deletion through replacement readiness and fresh mapping. |
| Component memory p95 | At or below 70% of the configured limit | Evaluated separately for the maximum agent replica and collector ratio in every steady sample. |
| Component CPU throttling p95 | Below 1% | Exclusive bound over per-interval counter deltas for the maximum agent replica and collector. |
| Agent scan and post failures | Delta 0 | Each aggregate counter must remain monotonic and must not increase during the sampled window. |
| Kubernetes API errors | Delta 0 | Cluster-wide API server HTTP `5xx` request counters must not increase during the sampled window. |
| Kubernetes API rate limiting | Delta 0 | Cluster-wide API server HTTP `429` request counters must not increase during the sampled window. |
| Node MemoryPressure | 0 Nodes | Every sample reads the Node condition. |
| Canary regression | At or below 5% | The RC observed median duration may be at most 5% slower than the control median. Lower is better. |

The development smoke still enforces profile identity, sample count, mapping, coverage and operational stability. Missing optional telemetry cannot turn into a pass; the corresponding check says `not_evaluated`. The `rc-5000` evaluator fails when any required measurement is missing or malformed.

## Measurement sources

The runner keeps the production network and metrics boundaries unchanged:

- `kubectl memlens status`, `doctor` and `top containers` provide reliability, mapping and CLI timing through the authenticated aggregated API.
- The TUI probe uses an 80 by 24 pseudo-terminal with `NO_COLOR=1`. It records elapsed time only.
- Agent scan duration, mapping and post counters come from one agent's loopback endpoint at a time through `kubectl port-forward`. The runner stops each forward before opening the next one. It does not add a Service or scrape annotation.
- On local kind, `hack/observe_kind_telemetry.py` uses argv-only `docker exec`, `crictl ps`, `crictl stats`, `crictl inspect` and a bounded cgroup v2 `cpu.stat` lookup. It emits aggregate agent and collector values only.
- Kubernetes Pod status supplies restart and OOM-kill deltas. Node conditions supply MemoryPressure.
- The API server `/metrics` response supplies cluster-wide `429` and `5xx` counters. Because these counters cover all clients, use a quiet disposable cluster and investigate any increase rather than attributing it automatically to KubeMemLens.

The kind observer keeps container IDs only in a mode `0600` temporary state file so it can calculate CPU counter deltas between samples. The sanitised output reports aggregate bytes and counters plus the maximum per-replica memory and CPU ratios. A counter reset fails the observation. It does not print Node, namespace, Pod, container or runtime identifiers.

## Control and observed canary

The runner creates one restricted canary Pod from the profile image. Each measurement executes:

```sh
dd if=/dev/zero of=/dev/null bs=1M count=<profile-canary-MiB>
```

Before the control series, the runner patches the agent DaemonSet with an unsatisfied node selector and waits for the agent Pods to stop. It takes seven control measurements while the density workload remains present. It then restores the exact original selector, waits for the agents and complete mapping, and records observed measurements during the steady-state soak.

The evaluator compares nearest-rank medians. This canary detects gross execution overhead in the tested cluster. It is not an application benchmark and does not support a claim about database, network or user-request latency.

## Disruption sequence

The development smoke runs the agent-disabled control phase and then replaces one exact workload staging batch starting from the full target density. It requires complete mapping within 120 seconds after each recovery starts. For `rc-5000`, the replacement batch is 10 Pods and 500 containers. The collector must expose the exact replacement Pod UIDs as fresh evidence; partial optional memory context does not make fresh UID mapping stale. This tests KubeMemLens recovery from bounded workload churn without treating a full 5,000-container runtime restart as a KubeMemLens latency measurement.

The `rc-5000` profile pauses and restores one real kind worker while the density workload is resident, then runs the [reliability failure harness](reliability.md). The combined sequence covers a workload-serving Node interruption, agent outage, collector restart, removed and replacement Node inventory, delegated-authorisation API failure, partial agent rollout and final recovery. Each recorded event must meet the 120-second budget. The collector generation and history reset remain subject to the single-collector contract.

Capacity rejection and degraded-state behaviour use the deterministic test gate:

```sh
make verify-scale-capacity
```

This focused gate checks 10,000-record paging and response bounds, store capacity rejection, authenticated ingestion sequence safety, scoped history loss and distinct TUI degraded states. It is synthetic correctness evidence, not a 10,000-container live claim.

## Prerequisites

- A clean checkout of the revision under test.
- A disposable kind cluster with KubeMemLens already installed and healthy.
- Enough Docker memory and schedulable Pod capacity for the selected profile, four kind Nodes, the canary and KubeMemLens. Record the tested Docker allocation; nested kind allocatable memory is not a host-capacity check.
- The default five-second agent interval for the checked-in scan budget.
- `curl`, Docker, Expect, Go, `jq`, `kubectl` and Python 3.
- Permission to create and delete the dedicated namespace and its workload Pods, patch and restore the agent DaemonSet, port-forward to agent Pods, read Node conditions and read API server metrics.
- A new or empty directory below `qualification-evidence/`.

The qualification profile requires local Docker access to every supplied kind Node container. It fails rather than estimating component telemetry from `kubectl top`.

The checked-in `hack/scale-profiles/kind-rc.yaml` defines one control-plane and three worker Nodes, each with `maxPods: 200`. It does not choose a Kubernetes version. Create the cluster with a separately reviewed, digest-pinned kind Node image, then install the exact KubeMemLens candidate as described in the [installation guide](installation.md):

```sh
kind create cluster \
  --name kube-memlens-scale \
  --config hack/scale-profiles/kind-rc.yaml \
  --image 'kindest/node:<reviewed-version>@sha256:<64-lowercase-hex-characters>'
```

## Run the development smoke

Choose the exact context and a new labelled namespace:

```sh
export SOAK_CONTEXT='kind-kube-memlens-e2e'
export SOAK_NAMESPACE='kube-memlens-soak-development'
export SOAK_ARTIFACT_DIR="$PWD/qualification-evidence/scale-development"
export SOAK_PROFILE_PATH='hack/scale-profiles/development-smoke.json'
export SOAK_ACKNOWLEDGE='run-and-remove-kube-memlens-density-soak'

make soak-live-density
```

The normal kind CI path uses this profile with its own temporary context and evidence directory.

## Run RC 5,000 qualification

Confirm the cluster has capacity for 100 density Pods, each with 50 containers, before acknowledging the run:

```sh
export SOAK_CONTEXT='kind-kube-memlens-scale'
export SOAK_NAMESPACE='kube-memlens-soak-rc-5000'
export SOAK_ARTIFACT_DIR="$PWD/qualification-evidence/scale-rc-5000"
export SOAK_PROFILE_PATH='hack/scale-profiles/rc-5000.json'
export SOAK_ACKNOWLEDGE='run-and-remove-kube-memlens-density-soak'

make soak-live-density
```

The runner evaluates the raw summary before it reports success. To repeat evaluation without touching the cluster:

```sh
python3 hack/scale-profiles/evaluate.py \
  --profile hack/scale-profiles/rc-5000.json \
  --summary qualification-evidence/scale-rc-5000/density-soak-summary.json \
  --output qualification-evidence/scale-rc-5000/density-soak-evaluation.json
```

An evaluator exit status of `0` means the supplied summary met the selected profile. Status `1` means one or more budgets failed. Status `2` means the profile or summary was malformed. A successful evaluation is necessary but does not by itself qualify an unrecorded environment or a different build.

## Evaluation records

The raw summary has schema version 1 and contains:

- profile ID and canonical digest;
- an identifier-free sampling probe name when orchestration stops during a measurement;
- the exact workload object copied from the profile;
- bounded aggregate samples with monotonic elapsed seconds for mapping, reliability, component resources, operational state and latency;
- agent scan and post measurements;
- expected and observed replacement Pod counts plus resident container counts before and after replacement;
- recovery seconds for the exact workload replacement batch, agent, collector, Node, API and partial rollout;
- API server counter deltas, Node MemoryPressure samples and canary series; and
- explicit privacy flags and caveats.

The evaluation has schema version 1, the selected profile identity, overall `pass` or `fail`, one record per budget and a plain failure list. Each check retains its budget and observed value. The evaluator uses nearest-rank percentiles and does not hide unavailable required telemetry.

The evidence directory uses mode `0700`; result files use mode `0600`. It is ignored by Git. If a sample acquisition exhausts its bounded read-only retries, `sample-failure.json` records only the phase, elapsed seconds and allowlisted probe step; latency probes are not retried. The evaluator requires all four privacy flags to be `false`, permits at most eight caveats of 240 characters each, and rejects identifier-bearing keys such as context, cluster, namespace, Node name, Pod name or UID, container ID, token and kubeconfig. Keep kubeconfigs, raw runtime output, port-forward logs, API metrics, Node and Pod JSON, cloud identifiers and failed raw diagnostics local.

A result may enter the repository or a durable release record only after manual privacy review. Retain the sanitised raw summary, evaluation, exact KubeMemLens source revision, runtime image digest, chart identity, kind and Kubernetes versions, host architecture, run date and environment limitations. If adding that record changes the commit, verify that no runtime, chart or profile input changed after the tested revision.

## Cleanup and recovery checks

The exit trap stops port-forwards, restores the original agent selector, requests deletion of the labelled soak namespace and writes a final summary. If the reliability subprocess fails, the evidence directory also retains an identifier-free `reliability-failure.json` with the final failed phase and the phase that triggered cleanup. The KubeMemLens installation remains in place.

After any run, verify cleanup rather than relying on the trap message:

```sh
kubectl --context "$SOAK_CONTEXT" get namespace "$SOAK_NAMESPACE"
kubectl --context "$SOAK_CONTEXT" -n kube-memlens get daemonset kube-memlens-agent -o jsonpath='{.spec.template.spec.nodeSelector}'
kubectl --context "$SOAK_CONTEXT" -n kube-memlens rollout status daemonset/kube-memlens-agent
go run ./cmd/kubectl-memlens --context "$SOAK_CONTEXT" --collector-namespace kube-memlens status
```

The namespace lookup should return NotFound. Compare the selector with the reviewed chart values. Confirm ready state and fresh evidence before reusing the cluster.

## Claim boundary

A development-smoke pass proves only that the small kind path worked. A local `rc-5000` pass can support a local 5,000-container, 30-minute result for the exact environment recorded. It does not prove:

- the 100,000-container store ceiling as live capacity;
- a 10,000-container live profile;
- provider, runtime, CNI or hardware combinations not tested;
- long-duration or durable history;
- high availability or zero-downtime upgrades;
- internet scale; or
- representative application performance beyond the canary method above.

Managed-provider support still requires the separate [existing-cluster qualification](qualification.md). Release notes must link both records when they claim both a provider profile and a live-scale profile.
