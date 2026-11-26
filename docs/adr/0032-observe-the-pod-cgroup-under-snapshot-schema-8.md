# ADR 0032: Observe the Pod cgroup under snapshot schema 8

Date: 26 November 2026

Status: accepted

## Context

KubeMemLens reads each container's cgroup but not the Pod's own cgroup, the
parent of the containers and the sandbox. It derives the Pod limit by adding
up the container limits. For a Pod that uses Kubernetes pod-level resources
that sum is wrong (issue 317). On Kubernetes 1.37 with containerd, a container
with no limit of its own carries the Pod limit in its `memory.max`, so the sum
counts the Pod limit again: a Pod limited to 96Mi with one 32Mi container
reads 128Mi. Where the runtime leaves such a container at `max`, the Pod reads
`unlimited`. An OOM at the Pod-level limit is counted in the parent's
`memory.events.local`, so it is not seen at all.

The parser already reads `zswap` and `zswapped` from `memory.stat` and
`oom_group_kill` from `memory.events` and `memory.events.local`, then drops
them (issue 321).

Released collectors, agents, clients and incident readers decode JSON
strictly. A new field breaks every one of them unless it is negotiated.
ADR 0005 set up that negotiation: a reader states the highest snapshot schema
it understands, and every response and agent post is projected to it.
ADR 0019 used schema 7 for optional Node topology, which travels in its own
component and is read at its own endpoint.

## Decision

Add snapshot schema 8. It is complete in one change and covers both issues.

- `PodCgroup` on each mapped container and on each Pod: the Pod cgroup's
  `memory.current`, `memory.max` and `memory.events.local` (`oom`,
  `oom_kill`, `max`). Every value has its own Known flag. `max` in
  `memory.max` is unlimited, not zero. Only local events are carried.
- On the memory breakdown, `zswap` (`known`, `poolBytes`, `swappedBytes`) and
  `oomGroupKill` (counter and local counter, each with a Known flag). Zswap is
  known only when both keys are present. `oom_group_kill` is known only when
  its key is present.
- All new fields are omitted when zero, so a value without the evidence is
  written exactly as schema 7 wrote it.

The agent reads each Pod cgroup at most once per scan, through the cgroup root
opened with `os.OpenRoot` and with the existing bounded file reads. It finds
the Pod cgroup from the path component that names the Pod UID. It attaches the
value only to containers that map to that Pod, where the scanner's grouping of
the directory is unambiguous and the UID in the cgroup path is the mapped
Pod's. The sandbox and unmapped cgroups get nothing. A missing file is
unknown. A Pod cgroup that disappears before it is read is unknown. Any other
read failure leaves that Pod's cgroup unknown and is counted as a cgroup read
error. It hides no container, so the agent still publishes the snapshot.

Pod-scoped data travels duplicated on each container, as Pod resources do.
The collector rejects schema 8 values in a lower schema, rejects values that
claim a number for something unknown, and requires the containers of one Pod
to agree. It computes every delta itself. Pod cgroup deltas are per Pod, from
the previous stored snapshot of the same node, and are the same on every
container of the Pod. A first sample has known deltas of zero. A counter that
goes down gives a zero delta.

Every read response is projected below schema 8, including the collector's
`/api/v1/namespaces` read and the extension's workload volume read, which were
not projected before. Agent posts are projected to the collector's schema.

One definition of the Pod limit, `api.PodSnapshot.EffectiveLimit()`: the Pod
cgroup's finite `memory.max`, with usage from its `memory.current`; otherwise
exactly the container sum used before. The Kubernetes-API resource budget
stays separate evidence and is never added to or used as a cgroup value.

Incident files keep none of this. Schemas 1 and 2 drop it, are marked partial
and record one caveat. Volume schema 5 drops it without a caveat, because its
released readers accept only a fixed list of caveats; a volume file that
carries it is refused.

Schema 8 sits on top of ADR 0005's mechanism unchanged. It does not touch the
schema 7 topology component of ADR 0019, which stays at its own endpoint. It
needs no new permission, host mount, endpoint or chart value: the agent
already reads these directories.

## Alternatives considered

- **A Pod list on the wire.** A second top-level array would need its own
  bounds, identity checks and size accounting, and would change the shape of
  the post. Duplicating on each container reuses the agreement check that Pod
  resources already have. A typical Pod cgroup adds about 260 bytes per
  container.
- **Reusing the full container breakdown for the Pod cgroup.** It has about a
  hundred fields, most of which repeat what the containers report. It would
  roughly double each post and invite adding the Pod's usage to its
  containers', which counts the sandbox and the containers twice.
- **One schema version per issue.** Two negotiation steps and two sets of
  compatibility rules for one release. The owner chose one schema in one
  change.
- **Carrying hierarchical Pod events.** They are the sum over the Pod's
  subtree. The containers already report theirs, so they add nothing and
  invite double counting.
- **Per-Pod HugeTLB now.** The `hugetlb.*` files are a separate controller with
  one file set per page size. ADR 0019 keeps HugeTLB evidence apart from
  ordinary memory values. It is left out of this schema and stays open on
  issue 321.

## Consequences

- A Pod with a finite Pod cgroup limit is measured against it, not shown as
  `unlimited`. Pod-scoped OOM events can be used as evidence.
- Older readers see exactly what they saw before.
- Each post grows by about 260 bytes per mapped container in a typical case
  and at most about 750 bytes.
- A Pod cgroup that cannot be read is counted in the node's cgroup read
  errors, which `doctor` reports, and that Pod falls back to the container-sum
  limit. The snapshot is not withheld: one unreadable parent file must not
  hide every container on the node.
- `/api/v1/namespaces` now reads the schema header. Without it the response is
  schema 1, which for namespace totals is what it served before. An invalid
  header is now refused there, as on the other reads.
- No incident file keeps the new evidence yet.

## Verification, migration and rollback

- **Newer agent, older collector.** The agent negotiates the collector's
  schema and projects its post, so the older strict decoder never sees a new
  field.
- **Newer collector, older agent.** The agent posts schema 7 or lower. Pods
  keep the container-sum limit.
- **Older CLI.** It sends its own schema, or none, and gets a projected
  response.
- **Saved incident files.** Schemas 1, 2 and 5 never contain the new fields,
  so older replay binaries read new files. New binaries read old files
  unchanged.
- **Rolling back the collector.** There is no stored data to migrate. A schema
  8 agent posting to the older collector is refused as invalid JSON. As
  ADR 0005 describes, the agent then renegotiates on the replacement
  collector's epoch and posts at its schema.
- **Checks.** Tests decode every projected response and agent post with a
  strict decoder of the schema 7 types and compare the bytes; fuzz targets
  cover the parser and the path rule; fixture scans cover each supported cgroup
  layout. The kind run, including the older-reader and mixed-rollout checks,
  is run separately before release.
