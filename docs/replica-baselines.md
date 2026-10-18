# Replica memory comparisons

Development builds can compare a Pod's current memory with other authorised Pods
under the same top-level workload. The feature is informational: it does not
change diagnosis severity, recommendations, resources or replica counts. It uses
local cgroup snapshots and bounded collector history; Prometheus is not required.

## Enable and authorise

Namespaces must already exist. Enable only the namespaces to be compared:

```yaml
replicaBaselines:
  enabled: true
  namespaces: [team-a]
```

Apply this values file through the installation's normal Helm upgrade. The chart
adds namespaced metadata source Roles for the collector and an **unbound**
`kube-memlens-replica-viewer` ClusterRole. Bind that role with a RoleBinding in each
approved tenant namespace. It grants the named memory comparison endpoints and
read-only Pod, controller, revision and Event metadata required to establish peers.
It grants no Node, Secret, exec or mutation access. Existing viewer roles do not
acquire these permissions automatically. For example, as an authorised administrator:

```bash
kubectl -n team-a create rolebinding memory-replica-readers \
  --clusterrole=kube-memlens-replica-viewer --group=team-a-memory-readers
```

For a standalone authenticated collector, use `--replica-baselines=true` and
`--replica-namespaces=team-a`. Empty, duplicate or wildcard scopes are rejected.

```bash
kubectl memlens replicas pod app-0 -n team-a
kubectl memlens replicas workload deployment/app -n team-a
kubectl memlens replicas workload deployment/app -n team-a -o json
```

In the TUI, select a Pod or workload and press **B**. Use **r** to refresh,
**j/k** or **PgUp/PgDown** to scroll, and **Esc** to close. A refresh clears the
previous result; cancelled and obsolete responses cannot replace it. Reports older
than 30 seconds display a stale message. JSON/YAML output includes authorised peer
names and should be handled as tenant data.

## Peer selection and confidence

Each candidate needs **four other eligible references**, so at least five
comparable Pods are required. Each query is limited to 16 Pods, eight declared
containers per Pod, 181 retained points per Pod and 256 KiB of encoded response.
Oversized selections fail explicitly. ReplicaSets are grouped under their verified
Deployment. CronJob comparisons keep separate Job executions apart. StatefulSet
and DaemonSet revisions require verified, owned ControllerRevision identities.
Standalone ReplicaSets and ReplicationControllers have no reliable template
revision in this source contract and therefore abstain.

Peers must have the same verified root UID, revision, immutable image digests,
container roles and memory resource layout, including observed cgroup enforcement.
Terminating, inactive, unready, stale, partial and mismatched replicas are excluded
with reasons. Unknown revision or image identity also excludes a Pod. Metadata is
resolved and caller permissions are checked again before any peer names or counts
are returned. Matching labels alone do not establish workload membership.

A five-minute stability window excludes known restart, resize and rollout changes.
Local comparison continuity starts after enabling the feature or restarting the
collector, and resets after contributor, Node identity or enforcement changes.
Partial Event history cannot prove that no change occurred: the confidence label
remains **limited change history** when that evidence is unknown. Pods with known
recent OOM, OOM-kill, memory-high/max events, PSI pressure or at least 90% container
limit usage are not reference peers. This does not establish that remaining peers
have equivalent traffic or are healthy in every respect.

## Calculation

For each candidate and metric, the candidate is excluded from its reference set.
The report shows the reference names, range, median, median absolute deviation
(MAD), candidate difference and confidence. It flags a difference only when all
applicable conditions hold:

- Absolute difference exceeds 25% of the absolute peer median.
- Absolute difference exceeds the practical effect floor below.
- For non-zero MAD, `abs(0.6745 * difference / MAD) > 3.5`.

Flat reference values use the first two conditions and explicitly omit a
statistical score. Confidence is an evidence-coverage description, not a probability.
The thresholds are versioned product heuristics, not kernel safety limits.

| Metric | Practical floor | Direction |
| --- | --- | --- |
| Cgroup charge or swap | 16 MiB | Charge: either; swap: higher |
| Anonymous, file-cache, shmem or kernel fraction | 10 percentage points | Either |
| Charge growth | 1 MiB/min | Either |
| Maximum container finite-limit utilisation | 10 percentage points | Higher |
| Maximum container PSI some / full | 1 / 0.1 percentage points | Higher |
| Known local OOM, OOM-kill, high or max event rate | 1 event/min | Higher |

Growth uses a bounded Theil–Sen median slope over actual samples in a common
six-minute lookback. It requires at least 12 samples spanning five minutes, no gap
above 30 seconds, start coverage within 30 seconds of the window and a last sample
within five seconds of the current snapshot. It never interpolates missing points.
Current samples must be no older than 30 seconds and peer clocks within five seconds.
Event-rate reference intervals must differ by at most 10% in duration and overlap
by at least 80%; incompatible intervals are omitted for that metric.

## Source limits and next checks

PSI and limit utilisation are **maximum-container** values requiring complete
contributor coverage. They do not measure Pod ancestor enforcement. Swap requires
explicit reported presence. The legacy wire does not distinguish absent composition
keys from zero: composition requires a positive reported component from every
contributor, otherwise that metric is unreported. Inconsistent independently sampled
composition is not clamped. Rates require explicitly known local event counters and
deltas; cumulative counts and absent hierarchical counters are not treated as rates.

Investigate different traffic or work performed, memory composition, pressure and
recent changes before drawing a conclusion about an outlier. Small peer sets and
missing metrics are abstentions, not evidence of normal behaviour.

## Disable and rollback

Set `replicaBaselines.enabled=false` and `replicaBaselines.namespaces=[]`, then
upgrade through the normal Helm workflow. The endpoints and optional source roles
are removed and continuity fingerprint work stops. Remove separately created viewer
RoleBindings if no longer needed. The collector restart also clears its ordinary
in-memory history. No persisted-data migration or privileged agent is introduced.

The [local qualification record](qualification-results/replica-baselines-local-kind-2026-09-29/README.md) covers the controlled outlier, access boundaries, terminal paths, rollback and cleanup. Managed-provider qualification remains separate.

See the [replica threat model](security/replica-baselines-threat-model.md).
