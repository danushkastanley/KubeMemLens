# ADR 0019: Isolate optional NUMA and HugeTLB context

Status: accepted

## Context

Specialised Nodes need NUMA distribution and HugeTLB pool/charge evidence without
changing ordinary memory accounting. Availability varies by kernel and cgroup
root. Kubelet availability may already account for hugepage capacity. Host topology
also requires a separate disclosure boundary and explicit acquisition opt-in.

## Decision

Use a bounded, read-only kernel adapter and a separate pure topology domain. Add
an explicitly negotiated schema-7 component to the existing authenticated Node
producer stream. Validate its identity and clocks before atomic admission with
the enclosing observation. Retain latest and last successful source values within
the existing Node record bound, preserving individual source times and excluding
topology from ordinary Node history.

Use a named, separately authorised Node endpoint and an unbound viewer role. Add
only opt-in read-only sysfs parents and a qualified cgroup root; retain the existing
non-root, capability-free producer. Do not collect detailed hardware identity or
walk workload cgroups. Keep the Pod view and ordinary memory values unchanged.

Treat NUMA distribution as an informational heuristic, with explicit freshness,
completeness and threshold requirements. Keep global pool reservation and usage,
per-domain pools, cgroup reservation accounting and limit failures distinct. Never
infer eviction, scheduling policy or access latency from these values.

Capture requires explicit opt-in. Incident schema 7 wraps schema 4, preserves
source/caveat evidence and aliases Node and NUMA identity by default. Existing
captures and old snapshot schemas retain their contracts. Topology comparison is
not offered because local domain aliases do not prove continuity across captures.

## Alternatives and consequences

Expanding the ordinary Node observation would enlarge every retained history point
and blur source semantics. A second producer or eBPF source would add identity or
privilege machinery for values already available through bounded read-only files.
The chosen design adds two small bounded records per owned Node and one optional
named read. A source failure can retain old evidence without refreshing its clock.
The parent mounts permit unsupported child interfaces but expose other metadata
within those parents to a compromised producer; opt-in deployment contains that
additional host visibility.

## Migration and rollback

Enable the profile only after qualifying its source roots and access. New clients
negotiate schema 7; older schemas omit topology. No persistent data migration is
required. Disable the profile to remove its mounts and endpoint/role while keeping
ordinary Node collection. See the [operator guide](../node-memory-topology.md),
[trust boundary](../security/node-topology.md) and
[local validation](../qualification-results/node-topology-local-linux-2026-09-29/README.md).
