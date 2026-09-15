# ADR 0014: Version OOM evidence streams

Status: accepted for the BPF-006 local prototype. See
[bounded qualification](../ebpf/OOM_LOCAL_QUALIFICATION.md); broader gates remain open.

## Context

Version 1 carries ephemeral OOM events but cannot carry typed OOM aggregates and
correlated evidence. Version 2 is explicitly limited to file/cache aggregates.
OOM evidence must distinguish local from hierarchical cgroup counters, finite
from unlimited limits, PSI stall from kill decisions, and Kubernetes context
from kernel scope. PID and command data must never enter aggregate retention.

## Decision

Use version 3 for the installed OOM kernel adapter. Keep version 1 unchanged and
keep the file/cache adapter on version 2. Installation selects the version per
accepted trace kind; requests cannot choose formats or programme identities.
A stream has exactly one version and one admitted target lifetime.

Version 3 carries bounded ephemeral victim events, a numeric OOM summary, and
separate cgroup and Kubernetes evidence. Scope counts are cgroup, global and
unknown. Missing process context is counted independently. Local and hierarchical
memory-event deltas, current bytes, limit states and PSI totals retain their own
meanings. Kubernetes restart/MemoryPressure samples do not classify a kernel OOM.
Unavailable, reset, uncertain and target-changed evidence cannot become zero.

Reserve 6 KiB for the terminal frame within the existing 8 KiB frame ceiling and
admitted total-byte budget. The private worker message ceiling remains 4 KiB;
Kubernetes context is assembled by the authenticated control service, which
already reads Pods and Nodes. No new RBAC or worker network access is required.
Authority loss suppresses rich terminal evidence. Target replacement terminates
the admitted lifetime rather than following a restarted or recreated target.

## Alternatives and consequences

Adding fields to versions 1 or 2 would change their strict canonical contracts.
A separate unbounded result endpoint would duplicate admission and retention
controls. Version 3 instead reuses the existing bounded stream and cleanup path;
older readers reject it explicitly, and old streams retain their prior shape.
Kernel hook coverage and missing context keep OOM summaries partial.

## Validation, migration and rollback

Verify version/kind isolation, canonical fields, maximum encoded sizes, event and
byte ceilings, permission loss, target replacement, missing data and counter resets.
Exercise controlled local OOM, tenant isolation and owned teardown only after the
exact new signed candidate is accepted. A global-OOM test needs a separate bounded
VM; shared-host exhaustion is prohibited. Independent review remains required.

Install a matched optional node/API image, worker and accepted programme policy.
No standard agent, chart, persisted incident schema or collector history changes.
Rollback replaces the complete optional installation and acceptance policy, or
removes OOM acceptance to disable new OOM traces. Verify owned cleanup first.
