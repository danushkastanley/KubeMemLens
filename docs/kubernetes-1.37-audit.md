# Kubernetes 1.37 release audit

Reviewed on 9 September 2026 against Kubernetes `v1.37.0` and KubeMemLens
`9b26658953a8c6aa1228441a14e97b750143e940`.

This records the upstream contract for subsequent memory features. Runtime
compatibility does not mean KubeMemLens already consumes every new API field.
At this audit's source baseline, the Kubernetes modules remained on `v0.36.4`.
The subsequent coordinated `v0.37.0` upgrade and authoriser adaptation are
recorded in the [changelog](../CHANGELOG.md#kubernetes-137-client-compatibility).

## Release and source evidence

The [upstream release](https://github.com/kubernetes/kubernetes/releases/tag/v1.37.0)
was published on 26 August 2026 and is not a prerelease. The published
[final changelog](https://github.com/kubernetes/kubernetes/blob/master/CHANGELOG/CHANGELOG-1.37.md#v1370)
contains the GA summary. The changelog inside the immutable `v1.37.0` source
tag still starts at rc.1, so it is not sufficient alone to establish release
status.

The [tagged feature registrations](https://github.com/kubernetes/kubernetes/blob/v1.37.0/pkg/features/kube_features.go)
are identical to rc.1. They settle the conflicting release notes about
`PodLevelResourceManagers`: its final default is **false**.

| Capability | Final contract | Required interpretation |
| --- | --- | --- |
| Metrics API | `metrics.k8s.io/v1` has released [types](https://github.com/kubernetes/metrics/blob/v0.37.0/pkg/apis/metrics/v1/types.go) | Discover the served version; CPU and working-set memory are distinct from cgroup composition. |
| PodLevelResources | Beta, enabled by default | Preserve Pod and container resource scopes; do not add overlapping budgets. |
| PodLevelResourceManagers | Beta, disabled by default | Pod-level resource specifications do not prove that resource managers are enabled. |
| MemoryQoS | Beta, enabled by default | Observe cgroup boundaries and event deltas; a gate alone does not prove throttling. |
| PodAndContainerStatsFromCRI | Beta, disabled by default | Preserve the reported source and tolerate omitted statistics. |
| HugepageAwareEviction | Beta, enabled by default | Hugepage-reserved RAM is excluded from kubelet memory availability; do not subtract it again. |
| CSIVolumeHealth | Alpha, disabled by default | Missing health status is unavailable or unreported, never evidence of health. |
| InPlacePodVerticalScalingMemoryBackedVolumes | Alpha, disabled by default | Do not require it for ordinary diagnosis or enable it in the standard chart. |

The [kubelet defaults](https://github.com/kubernetes/kubernetes/blob/v1.37.0/pkg/kubelet/apis/config/v1beta1/defaults.go)
leave `memoryThrottlingFactor` unset and default `memoryReservationPolicy` to
`None`. Protection, throttling and hard limits must remain separate concepts.
The final release also includes fixes for stale MemoryQoS controls after
disablement and Pod-level controls during resize.

The [core API](https://github.com/kubernetes/api/blob/v0.37.0/core/v1/types.go)
contains PVC `healthStatus` and Pod `volumeHealth`; the
[storage API](https://github.com/kubernetes/api/blob/v0.37.0/storage/v1/types.go)
contains CSINode `storageHealth`. Reading these fields does not establish driver
support. CSI specification, driver capability, feature gate, authorisation and
source freshness require separate checks before consuming health evidence.

The released [CSI 1.13.0 specification](https://github.com/container-storage-interface/spec/blob/v1.13.0/csi.proto)
contains `ControllerListVolumeHealth`, `ControllerGetVolumeHealth`,
`NodeGetVolumeHealth` and `NodeGetStorageHealth`. This satisfies the specification
prerequisite, but does not establish that any particular installed driver
implements those RPCs.

## Compatibility review

Kubernetes removes kubeadm `v1beta3`; the pinned kind `v0.32.0` supports
`v1beta4`. The Helm workloads are not static Pods and do not use the removed
static-Pod API-reference option or scheduling alpha APIs. SELinux mount
labelling changes require provider-specific evidence and do not change memory
accounting. Legacy cAdvisor interfaces removed upstream are not inputs to the
current direct cgroup collector.

The client upgrade must verify aggregated discovery: the final changelog says
`client-go` requests discovery `v2` rather than falling back to `v2beta1`.
No kubelet configuration, log, proxy or CSI socket access is added by this audit.

## Matrix and verification

The [CI matrix](../.github/workflows/ci.yml) already pins Kubernetes `1.35.5`,
`1.36.1` and `1.37.0`, immutable kind node images and matching Linux amd64
kubectl checksums. All three checksums were compared with `dl.k8s.io` during
this audit. The local lifecycle harness uses the same `1.37.0` image digest.
Historical 1.34 qualification remains available without extending the current
[support contract](compatibility.md).

[CI run 34324379433](https://github.com/danushkastanley/KubeMemLens/actions/runs/34324379433)
passed on the source commit above. Step-level readback confirms that all three
lanes actually ran install, diagnosis, upgrade, rollback and uninstall. The
1.37 lane also enabled density, TUI and authenticated isolation checks.

Local verification passed on 9 September 2026 using kind `v0.32.0`, Helm
`v3.18.4`, Kubernetes `v1.37.0`, containerd `v2.3.4`, Linux
`7.0.12-linuxkit` and arm64. The harness created a separate disposable cluster
and removed it after success. Existing local clusters were retained.

```sh
E2E_CLUSTER_NAME=kube-memlens-r2-k137-001 \
E2E_IMAGE=kube-memlens:r2-k137-001 \
E2E_ARTIFACT_DIR="$PWD/local-docs/product/r2-evidence/k137-001-local" \
E2E_RUN_LIVE_DENSITY_SMOKE=true \
E2E_RUN_TUI_SMOKE=true hack/e2e-kind.sh
```

Passed paths include authenticated installation, Helm connection hooks,
`doctor --strict`, filtered and machine-readable queries, explanation, history,
recommendation, live comparison, redacted capture/replay, metrics isolation,
upgrade, rollback, post-rollback diagnosis and uninstall cleanup.

The live PTY test used 20 Pods across three namespaces at `80x24`, `120x30` and
`180x50`, including scrolling, sorting, filtering, detail, refresh, pause and
recommendation. The density development profile used 20 containers, seven
steady-state samples over 30 seconds and replacement of both worker Pods. It
retained 100% workload mapping with no unexplained restarts or OOM kills. Agent
scan p99 was 18.412 ms, CLI p95 49 ms and TUI-fetch p95 62 ms. These small-profile
results do not extend the existing production scale qualification; recovery
fault injection is not evaluated by this profile.

Actionlint `v1.7.7`, YAML matrix/version/digest validation, upstream kubectl
checksum comparisons, the support-contract check, changed-document local links
and `git diff --check` passed. No runtime code changed in this audit.

## Scope and limits

The maintainer selected local testing for the R2 feature phase on 9 September
2026. No AWS, Google Cloud or Azure test resources are required or authorised by
this phase. Existing provider records remain historical, version-bound evidence;
this audit creates no new managed-provider or CSI-driver support claim.

Subsequent changes must retain the three-minor lifecycle checks, absent-field
compatibility, tenant isolation and redacted exports. Optional APIs remain
optional. This documentation change has no runtime, data or deployment effect
and can be reverted independently.

## Local Pod resource verification

`E2E_RUN_POD_RESOURCE_SMOKE=true hack/e2e-kind.sh` exercises effective Pod budgets,
Pod and container resize, rejected oversized requests, capture/replay,
comparison and an actual pre-extension CLI against the new collector. The test
uses one disposable namespace and checks that the Pod and containers did not
restart. The 1.37 CI lane enables it; older lanes retain their lifecycle checks.

For the optional memory-backed volume resize path, use a separate local run:

```sh
E2E_CLUSTER_NAME=kube-memlens-volume-resize \
E2E_KIND_CONFIG=hack/kind-profiles/pod-memory-resize.yaml \
E2E_RUN_POD_RESOURCE_SMOKE=true E2E_TEST_VOLUME_RESIZE=true hack/e2e-kind.sh
```

This explicit alpha profile also checks the fixture's tmpfs mount capacity.
It is not a default chart feature or a managed-provider qualification claim.

The released [PodResize admission plugin](https://github.com/kubernetes/kubernetes/blob/v1.37.0/plugin/pkg/admission/podresize/admission.go)
rejects requests above node allocatable before persisting the resize. The live
negative case checks that rejection and the unchanged applied budget. Mapper
fixtures separately verify pending, deferred, infeasible, errored and unknown
conditions, including simultaneous allocation/application generations.

The optional [resource-metrics source](resource-metrics-source.md) discovers served
versions. Metrics Server v0.9.0 still registers v1beta1; local controlled fixtures
verify the v1 and transition contracts without installing a provider.
