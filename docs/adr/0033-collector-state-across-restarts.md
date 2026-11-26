# ADR 0033: Collector state across restarts

Date: 26 November 2026

Status: direction decided on 26 November 2026; stage 1 is implemented, in the
first form listed under [What blocks stage 1](#what-blocks-stage-1), and
nothing else is. The
owner answered the deciding question: evidence must survive both a collector
container restart and a collector Pod replacement. That selects option D, after
stage 1 (option A). See [Decision](#decision). The choices still open are
listed under "Not yet decided".

## Context

Issue 319 (audit N-11) says that a collector restart loses snapshots, baselines
and history, and that this blocks post-mortems that outlive the Pod. Issue 310,
the `v1.2.0` roadmap item "OOMKill and eviction post-mortems that keep bounded
evidence after the Pod is gone" (`docs/roadmap.md:29`), is recorded as depending
on it (`docs/codebase-audit-2026-11.md:269`, `:412`).

The issue cites `docs/reliability.md:48`. On `main` the restart sentence is at
`docs/reliability.md:52`; line 48 is about last-known evidence.

### What the collector holds today

Every item below lives only in the collector process
([reliability contract](../reliability.md), `docs/reliability.md:52`;
`docs/security-model.md:63`).

| State | Where | Bound | After a restart |
| --- | --- | --- | --- |
| Current node snapshots | `internal/collector/store.go:32-65` | 5,000 nodes, 100,000 containers (`store.go:28-30`) | Rebuilt by the next agent post. Agents send a full snapshot on every scan (`cmd/memlens-agent/main.go:188-237`), every 5 seconds by default (`main.go:33`). |
| Event-delta baselines | The previous snapshot of the same container (`store.go:135-151`) | Same as snapshots | The first post after a restart has no baseline and says so (`DeltaWindowKnown=false`, `store.go:143-151`). The second post has one: about 10 seconds at the default interval. |
| Pod history | `internal/collector/history.go:26-35` | 15 minutes, 181 points per Pod, 1,000 series (`history.go:20-24`) | Starts empty. Reported partial until a continuous 15-minute window exists (`docs/reliability.md:60`). |
| Ingestion epoch, per-agent sequence, retired identities | `internal/extension/coordinator.go:36-55` | Agents up to the 5,000 node-record limit, doubled when Node context is on; retired up to four times that, for 2 minutes (`coordinator.go:75-80`, `cmd/memlens-collector/main.go:170-175`) | A new random epoch per process (`coordinator.go:84-91`). Agents get `epoch_mismatch`, refresh once and resubmit (`internal/agent/publisher.go:83-94`), within the same publish. |
| Node context and its history (optional, off by default) | `internal/collector/node_context.go:29-46` | 1,000 series, 61 points, 64 MiB, 15 minutes (`internal/nodecontext/limits.go:19-28`) | Rebuilt by the next 15-second Node-context collection; history over 15 minutes. |
| Volume context (optional, off by default) | `internal/collector/volume_context.go:20-28` | Byte-accounted per node | Rebuilt by the next Node-context post. |
| Incident sessions (optional, off by default) | `internal/incidentsession/store.go` | 64 sessions, 128 entries and 256 KiB each, 1 hour (`internal/incidentsession/model.go:17-19`, `:66`) | Lost and not rebuilt. ADR 0023 accepts this (`docs/adr/0023-retain-bounded-owned-incident-sessions.md:59`). |

Replica comparisons hold no store of their own; they read the Pod history above
(`internal/collector/replica_baselines.go:12-18`). No other retained
collector state was found.

The agent keeps nothing a collector could ask for again. Its scanner has no
memory between scans (`internal/agent/scanner.go:23-28`); the publisher keeps
only the epoch, the sequence and the schema version
(`internal/agent/publisher.go:26-29`). The collector rejects a snapshot older
than 2 minutes (`internal/collector/server.go:31`, `:293`).

A terminated container's last snapshot leaves the store at the node's next post,
by design (`store.go:99-102`). Its Pod's history series is keyed by Pod UID
(`history.go:84`) and stays until its newest point is older than the window
(`history.go:247-257`). So today, inside one collector generation, a deleted
Pod's Pod-level history, including OOM-kill event deltas
(`internal/api/types.go:204`), remains readable for up to 15 minutes. The last
termination reason comes from the Pod status through the agent
(`internal/kube/mapper.go:121`) and leaves with the Pod.

### How the collector is deployed

- One replica, enforced by the schema (`values.schema.json:201`) and the
  template (`templates/deployment.yaml:12-14`), and explained in
  `values.yaml:119-120`. `make check-replica-contract` checks replica
  *baselines*, not the collector replica count. Chart paths here are under
  `charts/kube-memlens/`.
- `Recreate` strategy, so one in-memory generation serves at a time
  (`templates/deployment.yaml:22-25`). The old Pod exits before the new one
  starts.
- Read-only root filesystem, enforced (`templates/deployment.yaml:9-11`,
  `:157`). Non-root user and group 65532 with `fsGroup: 65532`
  (`templates/deployment.yaml:40-46`). The only volumes are Secrets, a
  ConfigMap and a projected token (`templates/deployment.yaml:180-227`).
- No persistence value exists in `values.yaml` or `values.schema.json`. The
  installation guide says "The chart installs no CRDs or persistent volumes"
  (`docs/installation.md:184`).
- Memory request 192 Mi and limit 256 Mi (`values.yaml:147-152`), so the
  collector is Burstable and can be evicted under node memory pressure.

### What an operator sees after a restart

`status` prints the state (`rebuilding`), the generation and the history reset
time (`internal/cli/status.go:159-169`). Until agents post again, `doctor`
fails "agent coverage", either because no node has reported or because expected
nodes are missing (`internal/cli/doctor.go:120-125`). The second message points
at the agents, not at the restart. No `doctor` check says that the collector
restarted recently or that history before a given time is gone.

### Constraints already decided

- Roadmap principle "No new install cost": features reuse the existing agent,
  cgroup v2 path and RBAC, and more access needs a reviewed design
  (`docs/roadmap.md:13-15`).
- [Compatibility](../compatibility.md): "High-availability collector or durable
  history" is deferred (`docs/compatibility.md:96`); "no database, persistent
  volume, replica consensus or high-availability promise"
  (`docs/compatibility.md:138`).
- [ADR 0017](0017-query-identity-bound-memory-history.md): the optional remote
  history adapter reads only cAdvisor working set and RSS
  (`0017-query-identity-bound-memory-history.md:25-27`) and resolves the
  *current* Pod before querying (`:19-21`; `internal/kube/memory_history.go:90-95`).
  It rejected a database for storage and privacy reasons (`:50-51`).
- [ADR 0023](0023-retain-bounded-owned-incident-sessions.md): incident sessions
  are ephemeral; a database or persistent volume was rejected because it
  "would imply recovery and multi-instance routing"
  (`0023-retain-bounded-owned-incident-sessions.md:49-50`).
- ADR 0005: a snapshot wire change is negotiated through the ingestion epoch.

## The problem in operator terms

A Pod is OOM-killed or evicted at 10:00. The collector restarts at 10:05,
because it was itself evicted or OOM-killed, or because someone upgraded it. At
10:10 the operator asks what happened. Today the answer is "history reset at
10:05": the evidence that existed at 10:04 is gone, and only a capture taken
before 10:05 would have kept it.

Two separate needs hide in issue 319:

1. **Keep evidence after the Pod is gone.** This is issue 310's core need. It
   does not require persistence: it is a bounded in-memory retention rule
   inside one collector generation, which the collector partly has already for
   Pod history.
2. **Keep that evidence across a collector restart.** This is persistence. It
   matters only when a restart falls between the incident and the read.

The audit calls N-11 a prerequisite for N-02. The code says it is not: 310 can
ship with in-memory retention and an honest "lost on collector restart" caveat.
Persistence widens the cases 310 survives.

## Goals and non-goals

Goals:

- Say which restarts each option survives, in operator terms.
- Keep every bound explicit: count, bytes and time.
- Keep "unknown" honest: restored or missing evidence is labelled, never
  presented as continuous.
- Keep the default install free of new cost, access and resting places for data.

Non-goals:

- High availability, failover or more than one collector replica.
- Longer retention than the configured window.
- Restoring ingestion epochs. A fresh epoch per process is what lets the
  collector reject replays from an earlier generation (`coordinator.go:123-125`).
- Restoring current snapshots or delta baselines. Agents rebuild them in one and
  two scans (about 5 and 10 seconds); restoring them risks showing old values as
  current for no real gain.
- Designing issue 310's retention rule. That needs its own design.

## Options considered

Restart kinds used below: a *container restart* keeps the Pod (collector crash,
OOM kill, failed liveness probe). A *Pod replacement* creates a new Pod (upgrade,
`kubectl rollout restart`, eviction, drain). *Collector node loss* is the node
running the collector going away.

### A. Document and surface the loss better (baseline)

Keep the collector stateless. Add a `doctor` check that warns when the
collector generation is younger than the history window, naming the reset time
and what is lost. Say plainly in the post-mortem runbook that a capture before a
restart is the only durable record. Captures already outlive everything
(`docs/reliability.md:132`).

Nothing new survives a restart. No cost, no new data at rest. Small change.

### B. Warm hand-over between collectors

The outgoing collector sends its bounded state to the incoming one.

This does not fit the deployment. Under `Recreate` the old Pod is gone before
the new one exists, so there is nobody to hand over to. A hand-over needs both
running at once: `RollingUpdate` with a surge Pod, which is exactly what the
template comment forbids, two stores behind one Service with agents split
across two epochs (`templates/deployment.yaml:22-23`). It also needs a new
authenticated collector-to-collector channel: a port or sub-resource, a
NetworkPolicy rule (`templates/networkpolicy.yaml:13-24` allows only health and
extension ports today) and an identity to check. It helps only planned
restarts. A crash, OOM kill, eviction or node loss gets nothing, and those are
the restarts a post-mortem cares about. Planned restarts are where an operator
can already capture first. Large change for the least useful case.

### C1. Bounded on-disk state in an `emptyDir`

The collector writes its Pod history (and, once 310 defines it, its
terminated-container evidence) to a disk-backed `emptyDir` with a `sizeLimit`,
and reads it back at start.

- Survives container restarts only. Kubernetes deletes an `emptyDir` with its
  Pod, so upgrades, evictions, drains, `rollout restart` and node loss all lose
  it. A crash or OOM kill writes nothing at the moment of failure, so up to one
  checkpoint interval is lost.
- Needs no PVC, StorageClass, RBAC, port or NetworkPolicy change.
  `fsGroup: 65532` already lets the non-root user write a volume. The read-only
  root filesystem stays.
- Must not use `medium: Memory`: tmpfs pages are charged to the collector's
  memory limit.
- Privacy: namespace, Pod and node names, Pod UIDs and memory values rest on the
  node's ephemeral disk for the life of the Pod, at most one history window old.
  Node root and anyone with host access can read them; they could already read
  process memory. There is no application-level encryption, as for RAM today.
- Medium-small change: one optional volume, a versioned file format, a restore
  path that discards anything outside the window, and status fields.

### C2. Bounded on-disk state on a PersistentVolumeClaim

As C1, on a PVC.

- Survives container restarts, Pod replacements and upgrades. Collector node
  loss is survived only if the volume can attach where the new Pod lands; a
  zonal disk cannot follow a Pod to another zone. `Recreate` stays, because a
  `ReadWriteOnce` volume cannot attach to two Pods.
- New install cost: a StorageClass must exist, and the chart gains a PVC,
  breaking `docs/installation.md:184` and `docs/compatibility.md:138`.
- Privacy: the same identities rest on network storage, beyond the Pod's life,
  and possibly after uninstall, depending on reclaim policy. Encryption at rest
  depends on the StorageClass. Volume snapshots and backups can copy it. Anyone
  who can create Pods in the namespace can mount it.
- It reopens what ADR 0023 rejected (`0023-retain-bounded-owned-incident-sessions.md:49-50`).
- Medium change, plus provider qualification for attach, detach and zone
  behaviour.

### D. Rebuild from the agents

Each agent keeps a bounded window of its own node's evidence and replays it to a
new collector generation, detected by an epoch change (`publisher.go:83-94`).

- Survives container restarts, Pod replacements, upgrades and collector node
  loss, because the evidence lives on the other nodes. Lost when the agent on
  the evidence node restarts or that node goes away. An OOM-killed Pod's node
  usually keeps its agent, so this matches 310 well.
- Agent memory: an estimate, for Pod-level history like the collector's, is
  110 Pods (the kubelet default maximum) × 181 points × 248 bytes = 4,937,880
  bytes, about 4.7 MiB per node, against a 128 Mi agent limit
  (`values.yaml:88-93`).
- Wire: the same estimate in JSON is about 110 × 181 × 614 bytes ≈ 12.2 MB per
  node, above the 8 MiB snapshot limit (`values.yaml:139`), so replay must be
  paged. Replayed points are older than the 2-minute age limit
  (`server.go:293`), so they need a separate, authenticated, node-bound backfill
  path with its own bounds, negotiated under ADR 0005.
- No new resting place: the data stays in agent memory on the node it came from.
  No new RBAC, volume or port.
- Large change: agent buffer, wire contract, schema negotiation, collector
  backfill and replay protection.

### E. Rely on the optional external history source

The memory history adapter (ADR 0017) is off by default (`values.yaml:52-53`).
When enabled it gives trend continuity for live Pods across a collector restart,
because the operator's Prometheus holds the data. It does not fit 310: it reads
only working set and RSS, not cgroup composition or events, and it resolves the
current Pod before querying (`internal/kube/memory_history.go:90-95`), so a
deleted Pod returns unavailable. Using it for post-mortems would mean changing
ADR 0017's identity rule. It costs nothing new to keep as it is.

### What survives, by option

H is the Pod history window; T is terminated-container evidence once 310
defines it; S is incident sessions. In every option current snapshots and delta
baselines are rebuilt from agents (about 5 and 10 seconds) rather than restored,
and the ingestion epoch is always new.

| Option | Container restart | Pod replacement, same node | Collector node loss | Upgrade |
| --- | --- | --- | --- | --- |
| A | Nothing | Nothing | Nothing | Nothing |
| B | Nothing | H, T, S if planned | Nothing | H, T, S |
| C1 | H, T, S if included | Nothing | Nothing | Nothing |
| C2 | H, T, S if included | H, T, S if included | Only if the volume reattaches | H, T, S if included |
| D | H, T from running agents | Same | Same | Same |
| E | Working set and RSS of current Pods, if enabled | Same | Same | Same |

### Cost, by option

Sizes use the defaults. A history point is 248 bytes in memory (measured with
`unsafe.Sizeof` on a copy of `api.MemoryHistoryPoint`, `internal/api/types.go:183-213`)
and about 614 bytes as JSON for one typical point (an estimate). At the ceiling
of 1,000 series × 181 points = 181,000 points, that is about 44.9 MB (42.8 MiB)
fixed-width or 111 MB (106 MiB) as JSON. The same history is already held in
memory today.

| Option | Memory | Disk | Wire | Chart values (default) | RBAC, volumes, ports | Identities at rest | Rollback |
| --- | --- | --- | --- | --- | --- | --- | --- |
| A | None | None | None | None | None | None new | Revert |
| B | Transient copy, up to ~43 MiB | None | New channel, up to ~43 MiB per hand-over | Strategy change | New port or sub-resource, NetworkPolicy rule, peer identity | In transit between Pods | Revert chart and image |
| C1 | Write buffer | ≤ ~43 MiB at the ceiling with a compact format (estimate) | None | One enable value, default proposed `false` | One `emptyDir` | Node ephemeral disk, Pod lifetime, ≤ 15 min old | Disable; the volume goes with the Pod |
| C2 | Write buffer | PVC size; needs a StorageClass | None | Enable, StorageClass, size | One PVC | Network storage, beyond Pod, maybe after uninstall | Disable; PVC deletion is a manual step |
| D | ~4.7 MiB per agent at 110 Pods (estimate) | None | Paged replay, ~12 MB per node at the ceiling (estimate) | Possibly agent memory | None | Agent memory only | Old agents and collectors ignore the new path, if negotiated |
| E | None | Operator's | Operator's | Existing `memoryHistory.*`, `false` | Existing optional RBAC | Operator's Prometheus | Existing |

A full C1 checkpoint at the ceiling written every 30 seconds would be about
1.4 MiB/s of disk writes (estimate). The format would need to append and compact,
or checkpoint less often, and the checkpoint interval is then the crash-loss
bound.

## Recommendation

The smallest option that gives issue 310 what it needs is A, plus 310's own
in-memory retention, with no persistence. 310 needs evidence after the Pod is
gone. That fits inside one collector generation, and the collector already keeps
a deleted Pod's history for up to 15 minutes. A collector restart in that window
then loses the post-mortem, and A makes that visible instead of silent.

If the owner wants post-mortems to survive collector restarts too, C1 and D are
the two close candidates, and one question decides between them:

**Must post-mortem evidence survive a collector Pod replacement (upgrade,
eviction, drain, node loss), or is a collector container restart (crash, OOM
kill, failed probe) enough?**

- Container restart is enough: choose C1. It is the smallest persistence change,
  adds no install cost, RBAC, port or StorageClass, and keeps data on the node
  for no longer than the Pod. It does **not** survive an upgrade, an eviction, a
  drain, `rollout restart` or node loss, and a crash loses up to one checkpoint
  interval.
- Pod replacement must be survived: choose D. It keeps the no-new-install-cost
  principle and adds no resting place, but it is the largest change, it costs
  agent memory on every node, and it loses evidence when the evidence node's
  agent restarts.

B does not fit `Recreate` and does not help crashes. C2 breaks the no-install-
cost principle and two documented promises, and adds a resting place beyond the
Pod's life; it should be considered only if D proves too costly. E does not
serve deleted Pods.

There is no data on how often collectors restart in each way, so the choice
cannot be made from evidence today.

### Decision

On 26 November 2026 the owner answered the deciding question: evidence must be
kept across both kinds of restart, because either one can lose every post-mortem
with no fault on the operator's side. By the rule above that selects **D**:
agents keep a bounded window of their own node's evidence and replay it to a new
collector generation. C1 is not built, because it loses everything on a Pod
replacement. C2 stays rejected unless D proves too costly.

What D does not cover, stated so that nobody reads the decision as "nothing is
ever lost":

- An agent that restarts loses its own buffered window, and a node that goes
  away takes its window with it.
- A chart upgrade that changes the image replaces the collector and rolls the
  agent DaemonSet in the same release. An agent that restarts before the new
  collector has taken its replay loses that node's window. The table above says an upgrade is survived
  "from running agents"; how much that is during a combined roll is not
  measured, and the stage 3 restart test must measure it.

### Staged path

1. **First pull request (A).** A `doctor` check that warns when the collector
   generation started less than one history window ago, naming the reset time,
   and a runbook sentence saying that only a capture survives a restart. No
   chart, wire or storage change. Its restart test: in a disposable kind
   cluster, restart the collector and see `doctor` warn with the new reset time,
   then pass once the window has elapsed.

   This stage could not be built as written. It was built as a passing
   `doctor` line instead; see [What blocks stage 1](#what-blocks-stage-1).
2. **Issue 310's retention rule**, in its own design: what terminated-container
   evidence is kept in memory, for how long, and under what count and byte
   limits.
3. **D**, in its own pull requests, after stage 2 has said what
   terminated-container evidence exists to replay. Its restart test, in a
   disposable kind cluster: record history for a fixture Pod for at least two
   minutes; replace the collector Pod; assert a new generation, the points from
   before the replacement present and marked as replayed, no point older than
   the window, and the gap reported. Then repeat with a collector container
   restart, with an agent restart (that node's window is gone, and says so),
   and with a chart upgrade that rolls both, recording how many nodes kept
   their window.

   C1 was the alternative, and is not built. Its documented bound would have
   been: "After a collector container restart,
   restored history covers at most the configured window (default 15 minutes,
   181 points per Pod, 1,000 series), less at most one checkpoint interval.
   Nothing survives a Pod replacement." Its restart test records history for a
   fixture Pod for at least two minutes, stops the collector container but not
   the Pod, and asserts: the same Pod UID and a new generation; points from
   before the restart are present; no point is older than the window; the gap
   is reported and completeness stays partial until a continuous window exists
   (`history.go:115-117`). A second step replaces the Pod and asserts an empty,
   `rebuilding` history, proving the stated limit.

### What blocks stage 1

Found on 26 November 2026, when the work was started and stopped before any
code was written.

- **A warning would fail the readiness gates.** `doctor --strict` treats a
  warning as a failure (`internal/cli/doctor.go:259-266`). `hack/e2e-kind.sh`
  and `hack/release/verify_consumer.sh` wait at most two minutes for it to pass
  after install and again after rollback, and `hack/qualify-cluster.sh` requires
  every check to be `pass`. A warning that lasts one history window after every
  collector start would fail all three.
- **The CLI cannot see the history window.** It is set by the collector flag
  `-history-duration` (`cmd/memlens-collector/main.go:69`) and no response
  carries it. Clients decode responses strictly, so adding a field is a wire
  change for older CLIs, which stage 1 promised not to make.

The forms stage 1 could take instead. The owner chose the first on
26 November 2026, and it is implemented: `doctor` prints a `collector history`
line that always passes (`internal/cli/doctor_collector_history.go`), and the
restart test in the staged path was run with that line in place of a warning.

1. A passing `doctor` check whose summary names the generation start and says
   that no history or baseline from before it is held. No gate or wire change;
   it informs and does not warn. It would use the 15-minute default as "recent",
   as `internal/explain/explain.go:140` and `internal/cli/node_commands.go:58`
   already do.
2. No `doctor` change. `status` already prints the generation, the history reset
   time and history completeness; add the runbook sentence only.
3. A warning, with the window reported by the collector. That needs a
   negotiated response field and a reviewed change to the three gates above.

## Consequences

- With A only, the documented contract does not change. Operators are told
  sooner and more plainly what a restart cost them.
- With C1, `status` and the history reliability block must say whether history
  was restored, from when and with what gap. `resetAt` alone would then
  mislead. An unreadable or unknown-version file must be discarded and reported,
  never partly trusted.
- With D, the wire contract and both components change together, and mixed
  versions must negotiate or ignore the backfill.
- Any persistence makes `docs/reliability.md`, `docs/compatibility.md` and
  `docs/security-model.md` wrong as written, and each must change in the same
  pull request.
- Incident sessions stay ephemeral in every option unless ADR 0023 is revisited.
  Their records carry an authenticated owner and namespace binding that a
  restored file cannot re-prove.

## Verification, migration and rollback

This ADR changes no code, chart, stored data or wire format. Each stage brings
its own verification, following [docs/verification.md](../verification.md): a
real restart in a disposable kind cluster, not a unit test on mocks.

- A: removing the `doctor` check restores today's output.
- C1: a versioned file format; an older collector image ignores the directory;
  turning the value off removes the volume with the next Pod.
- D: negotiated under ADR 0005; an older collector rejects or ignores the
  backfill path and agents drop their buffer.

## Not yet decided

- Whether the agent buffer is on by default. It writes nothing to disk, but it
  costs memory on every node (an estimated 4.7 MiB at 110 Pods).
- The buffer's bounds per agent: Pods, points and bytes, and what happens when
  a node runs more Pods than the bound.
- What D replays first: Pod history only, or also the terminated-container
  evidence that stage 2 defines.
- How much evidence a combined collector and agent roll keeps, and whether the
  chart should order the two.
- Whether incident sessions should ever be included. This ADR proposes not.
- Names of any new chart values.
- Whether `resetAt` keeps its meaning and a new field reports restoration, or
  the other way round.

## Sources

- [Kubernetes `emptyDir` volumes](https://kubernetes.io/docs/concepts/storage/volumes/#emptydir):
  data is kept across container crashes and deleted with the Pod.
- [Reliability contract](../reliability.md), [compatibility](../compatibility.md),
  [security model](../security-model.md), [installation](../installation.md),
  [roadmap](../roadmap.md).
