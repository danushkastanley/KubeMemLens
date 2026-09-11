# Restricted incidents

The development build captures current Kubernetes observations without an agent
or collector. The caller needs the same scoped read permissions as restricted
`top`; optional source failures remain visible as partial or unavailable data.

```sh
kubectl memlens --mode=restricted capture -n team-a --pod api -o before.json
kubectl memlens --mode=restricted capture -n team-a --pod api -o after.json
kubectl memlens replay before.json --pod team-a/api
kubectl memlens compare --before before.json --after after.json --pod team-a/api
kubectl memlens compare --before before.json --after after.json --workload team-a/Deployment/api
kubectl memlens --mode=restricted compare api-a api-b -n team-a
```

TUI `C` asks for an explicit file destination. `x` marks the selected Pod and
compares it with the next selection. A container selection compares or captures
its parent Pod. An unreadable current source blocks new incident actions. A
source failure never authorises a wider namespace or a different credential.

## Schema and memory meaning

Restricted incident schema **3** contains `capturedAt`, `toolVersion`, `redacted`
and `observations`. The observation batch declares `mode: restricted`,
`receivedAt`, completeness, scoped source reports, Pods, Nodes and derived
namespace/workload groups. Containers retain resource and status context.

Working-set `bytes` is nullable: zero is a measured value, while null is missing.
Each measurement retains its API version, sample/receive times, window,
freshness, completeness and coverage. Group sums cover observed containers;
Node working set is a separate Metrics API quantity. Scoped captures omit Node
rows and rebuild groups from the selected Pods, with an explicit scope caveat.

Replay validates source, scope, schema, quantities, times, entity bounds and
aggregate coverage before rendering. It rejects unknown/trailing JSON, cgroup
payloads and terminal control characters. It uses the recorded capture time for
age and freshness text, so repeated offline replay is deterministic.

Comparisons show source versions, sample ranges, windows and coverage. A numeric
delta requires measurements from the same API version and matching coverage.
Partial, missing, stale, recreated and unconfirmed-identity observations keep
their caveats. Redacted names cannot prove continuity across captures; even
retained object UIDs do not turn working-set samples into counter history.
Workload rollups report when individual sample windows are unavailable in the
aggregate. Composition, local OOM deltas, PSI, trace and history require deep
evidence. Working set is never compared numerically with cgroup charge.

Deep captures retain schemas **1/2** and their existing readers. Restricted
`--schema-version=1` or `2` and `--include-history` fail before writing.
`--schema-version=3` requires restricted evidence. Schema 0 selects the format
for the selected source. Older binaries reject schema 3; retain the current
binary when archiving these captures. Collector storage has no migration.

## Privacy and file handling

Captures use mode `0600` and a 64 MiB read/write limit, including stdout. Staged
files are removed after success or failure. Publishing without `--force` fails
atomically if a destination exists. Explicit replacement uses an atomic rename;
a failed publication preserves the previous file. A destination symlink is
replaced only with explicit overwrite and its target is never followed.

Default exports remove Pod/Node UIDs and label maps. `--include-sensitive`
retains these for local investigation. Container IDs, cgroup paths, credentials
and kubeconfigs are absent from the restricted incident model. Kubernetes
display names and resource context remain present: captures are redacted, not
anonymous. Retention and deletion belong to the operator.

The [agentless local test](../hack/verify-agentless-kind.sh) exercises CLI and
PTY capture, replay, comparison, missing metrics and access revocation. Provider
support requires the separate [qualification process](restricted-qualification.md).
