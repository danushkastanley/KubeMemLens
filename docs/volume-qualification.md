# Local volume qualification

This development workflow measures the optional volume profile on one disposable
kind Node with a pinned hostpath CSI driver. It does not qualify EBS, GCE PD,
Azure Disk or a managed runtime. Controller conditions come from a controlled
Kubernetes API fixture; no external health-monitor sidecar is installed.
Storage-operation latency is outside this qualification.

Build the CLI, collector, producers and chart from the same reviewed source.
The workflow builds those components itself and compares running collector,
producer and driver executables with the built binaries. It records source,
chart, image, driver, registrar and runtime identities. Existing published images
must not be substituted for the development build used by this profile.

## Run locally

Use Docker, kind, kubectl, Helm, Go, Python 3 and Expect. Start from the repository
root. Preview the frozen profile without creating resources:

```sh
hack/qualify-volume-kind.sh --dry-run
```

Run the complete workflow with a new evidence directory:

```sh
VOLUME_QUALIFICATION_ACKNOWLEDGE=create-and-remove-local-csi-fixture \
VOLUME_QUALIFICATION_ARTIFACT_DIR=/absolute/path/to/new-volume-evidence \
  hack/qualify-volume-kind.sh
```

The runner rejects existing clusters and evidence files, uses its own kubeconfig,
and removes its cluster, local images and temporary credentials. It never uses
the current kubectl context. Existing unrelated clusters are left alone. Failure
keeps available sanitised observations for investigation; a failed run cannot
produce a passing qualification by omitting cleanup or measurements.

The profile is copied into the evidence directory before any measurement. Its
digest binds every observation. Do not change budgets to turn a failed run into
a pass. Investigate the failure and retain its evidence. A new workload or budget
requires an explicit new profile and review.

## Reference workload and measurements

Both windows use the same CSI backend, workload identities, Kubernetes gate and
observer. The reference has one persistent Pod and two Deployment replicas. Each
replica also writes 2 MiB into its own memory-backed emptyDir. The CSI test backend
is an isolated 128 MiB tmpfs filesystem. It is deliberately different from a real
cloud disk and cannot establish provider performance.

The baseline keeps Node collection active and disables volume parsing and health.
The enabled window adds volume parsing, health and a Pod/workload operator query
at each sample. Each phase settles for 30 seconds. The baseline lasts 120 seconds;
the enabled window lasts 300 seconds, sampled every 15 seconds.

| Measurement | Frozen limit |
| --- | --- |
| Sampled collector Go heap objects | 256 MiB peak; 64 MiB increase over baseline |
| Collector mean CPU increase | 100 millicores |
| Producer mean CPU / sampled charged memory | 50 millicores / 64 MiB |
| Kubelet mean CPU / sampled charged-memory increase | 100 millicores / 64 MiB |
| Sampled source read / delivery p95 | 1 second each |
| Pod / workload command p95 | 2 / 5 seconds |
| Related API read/access-review rate | 20 operations/second |
| Persisted API write-rate increase | 0.1 operations/second |
| Usage entry replacements | 6/minute |
| Unchanged health payload writes during the window | 0 |
| Accounted usage / health retention | 48 / 16 MiB |
| Summary response body | 4 MiB |
| Observed recovery | 120 seconds |

These are small-reference acceptance limits, not large-cluster sizing guidance.
Heap objects include live objects and objects not yet reclaimed by Go's garbage
collector. This is distinct from RSS, cgroup charge and accounted retention.
Sampled peaks can miss short-lived peaks. CPU comes from the owned processes'
cgroup counters. Counter resets, component replacement, missing samples and
incomplete windows fail validation.

Source and delivery durations are sampled only when their corresponding counters
advance. Delivery includes the production publisher's transport and collector
acknowledgement. Operator duration includes the real CLI and its authenticated
reads. API counters come from the owned API server and cover related Kubernetes
resources and access reviews. Persisted Pod/PVC/PV/CSINode writes are separate
from non-persisted access-review POSTs. These counters describe the whole isolated
fixture; they do not attribute every request to one product component.

## Functional and failure cases

The workflow uses the production doctor, API, CLI and TUI. It checks controlled
byte and inode changes, independent tmpfs evidence, shared-PVC deduplication,
private capture, offline replay/comparison, legacy export, compact and wide
terminal interaction, namespace isolation and permission revocation.

Separate cases raise filesystem and inode usage above the explanation thresholds
on the owned backend. The writer has an explicit 256 MiB memory limit. The test
checks the mount type and 128 MiB capacity before temporarily setting a 128-inode
ceiling, then removes only its own files and restores that ceiling. These cases
run outside the steady-state cost windows.

Driver-originated Pod/backend health and API-seeded controller conditions retain
separate provenance. Conflicts, recovery, stale/expired evidence, Pod replacement,
collector/producer restart and profile rollback are checked. The CI matrix also
retains the Kubernetes 1.36 gate-off workflow. Its missing alpha data must remain
unreported, without losing filesystem or memory diagnosis.

## Evidence and review

The bundle contains the frozen profile, source/build manifest, both measurement
windows, a draft record, the cleanup-confirmed record and the evaluator result.
Raw metrics, kubeconfigs, workload names, volume handles and diagnostic responses
stay in the private temporary directory. Public records contain bounded numbers,
fixed outcomes, versions and digests. The evaluator rejects private identifiers,
unknown schema fields, false controller provenance, wrong artefacts, stale source
times and incomplete cleanup.

Re-evaluate a completed bundle:

```sh
python3 hack/volume-qualification/evaluate.py \
  --profile /path/to/evidence/volume-profile.json \
  --source /path/to/evidence/volume-source.json \
  --evidence /path/to/evidence/volume-qualification.json \
  --output /path/to/new-evaluation.json
```

A dirty-tree run may demonstrate the measurement workflow, but remains ineligible
for review. Accept results only after independently checking the source/build
binding, raw sanitised observations, functional receipts and cleanup, including
clean-source CI reproduction. `managedProviderQualified` and
`storageOperationLatencyMeasured` remain false even when the local outcome passes.
No provider row is promoted automatically.

See [volume context](volume-context.md) for operator permissions and rollback.
Disabling the optional profiles removes their collection and read paths. There
is no data migration or automatic storage repair.
