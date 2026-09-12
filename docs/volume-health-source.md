# CSI volume-health source

The opt-in volume-health reader prepares the status contract for later volume
correlation. Existing memory collection, CLI/TUI views, collector snapshots,
metrics and captures do not invoke it. The standard chart gains no permissions.

`client.NewVolumeHealthSource` accepts a Kubernetes API connection and one
namespace. `Query(ctx, podName)` reads the current Pod using the operator's
credentials. `Report.Interactive()` exposes authorised observations in process;
serialising the report produces aggregate counts without names or condition text.
There is no new public endpoint or persisted schema.

## Sources and meaning

The released [core API](https://github.com/kubernetes/api/blob/v0.37.0/core/v1/types.go)
and [storage API](https://github.com/kubernetes/api/blob/v0.37.0/storage/v1/types.go)
provide three independent reports:

| Source | Kubernetes field | Scope |
| --- | --- | --- |
| CSI node plugin | Pod `status.volumeHealth` | Named Pod volume |
| CSI controller plugin | PVC `status.healthStatus` | Referenced claim |
| CSI backend | CSINode `status.storageHealth` | Matching driver on the scheduled node |

A present empty report means no adverse condition was reported. A missing report
is **unreported**, including on older clusters, after some recovery transitions,
or when a gate, kubelet, driver or sidecar does not provide the field. Absence
never establishes health or a specific missing capability. The model can represent
explicitly known `disabled` and `driver-unsupported` evidence; the API reader does
not infer either from missing fields. A verified non-CSI PV has a distinct
`not-csi-volume` reason. Permission failures remain separate from absent reports.

Future condition status strings remain visible to authorised consumers and count
as adverse, with `unknownStatus` set. Controller and node results never overwrite
one another. Storage access-mode and volume-mode qualifiers stay attached to their
own backend conditions; backend health is not copied into volume health.

`observedAt` records the API observation. Observations older than two minutes are
stale when evaluated again; retained adverse conditions still count as adverse.
Missing or future observation times have unknown freshness. Kubernetes transition
times describe changes, not successful probes. **Probe freshness is always
unknown** because these fields contain no heartbeat. A recent API read cannot
prove that the driver has recently probed a volume.

The final kubelet [health selection code](https://github.com/kubernetes/kubernetes/blob/v1.37.0/pkg/kubelet/volumemanager/cache/volume_health.go)
skips inline CSI volumes without a PV handle. They may have backend evidence while
Pod-volume health remains unreported. No health state is inferred from mounting.

## Authorisation and bounds

The reader performs only object GETs, with a five-second default query deadline
(maximum one minute), at most 64 Pod volumes, 16 conditions per report, 128 driver
entries, a 1 MiB response ceiling and an 8 MiB cumulative query ceiling. It rejects
redirects, malformed JSON, mismatched object identities, duplicate reports and
responses outside the bound namespace. Cancellation discards the query result.

Every referenced PVC is read with the same caller identity. Generic ephemeral
claims must have the current Pod's controller owner reference. A PV join requires
an authorised GET and a claim reference matching namespace, name and UID. Only
the verified PV driver, or the driver explicitly named by an inline CSI volume,
can select a CSINode report. The CSINode GET needs separate authorisation, and only
the matching registered driver is returned. No volume handle, backend address or
unrelated driver's condition is exposed.

The optional existing Pod informer supplies a copy only after a successful live
Pod GET matches namespace, UID, resource version and node. It cannot bypass
revocation or add stale data from a recreated Pod. No caller results are cached
between queries. Future collector integration must retain its tenant-authorised
boundary; passing a privileged collector identity as an operator is not supported.

Condition messages are capped at 1,024 UTF-8 bytes and reasons at 256 bytes, with
control characters replaced and truncation indicated. Default report exports
contain counts only. Identity and condition fields are also excluded from their
own default JSON representations. There are no new metric labels, logging paths
or redacted-capture fields containing these details.

Threats addressed here are cross-namespace joins, stale/recreated-object joins,
privileged informer fallback, untrusted driver text, oversized API responses and
misleading health inferred from missing alpha data. Kubernetes authorises each
GET separately; the query is not a transaction or a guarantee against permission
changes after a completed read.

## Local verification and rollback

Run the bounded unit/race tests with:

```sh
go test -race ./internal/volumehealth ./internal/kube ./internal/client
```

The disposable kind harness tests alpha-off and alpha-enabled Kubernetes 1.37,
namespace denial, denied node reads, and independent degradation/recovery:

```sh
CSI_HEALTH_ACKNOWLEDGE=create-and-remove-local-csi-clusters \
CSI_HEALTH_ARTIFACT_DIR=/tmp/kube-memlens-csi-evidence \
hack/verify-volume-health-kind.sh
```

It checksum-verifies and builds the upstream hostpath driver at
[`eccd681b18a2c96332f33c2cac5db38656edd500`](https://github.com/kubernetes-csi/csi-driver-host-path/tree/eccd681b18a2c96332f33c2cac5db38656edd500),
which implements the released CSI 1.13 health calls. Released hostpath v1.18.0
lacks those calls. The driver and registrar run only in disposable clusters.
The fixture image defaults to a non-root user. Its driver Pod explicitly uses
root and mount privileges because the upstream mount implementation and registrar
need them; both containers have read-only root filesystems and no API token.
Only the expected privileged-driver finding in that exact fixture path has a
[documented scanner exception](../hack/fixtures/csi-health/trivy-exceptions.yaml).
The same finding remains a failure outside that path. Workload Pods run non-root
with read-only root filesystems. Production image and chart checks are unchanged.

A real inline hostpath volume seeds a static PV/PVC for the health test; this is
not a dynamic provisioning or capacity qualification. Node and backend conditions
come from the driver through kubelet. Controller conditions are explicit Kubernetes
API fixtures, not a qualification of the external health-monitor sidecar.

No cloud provider, managed CSI driver, direct CSI socket reader or remediation is
part of this feature. Reverting the optional reader and model removes enrichment
without changing cgroup diagnosis, stored data or existing permissions.
