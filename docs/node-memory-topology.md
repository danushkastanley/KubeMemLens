# Optional NUMA and HugeTLB context

NUMA and HugeTLB context helps investigate specialised Linux Nodes. It is separate
from ordinary Node memory, Pod memory and diagnosis severity. It does not change
scheduling, memory policies, pool sizes or kubelet eviction calculations.

Enable the qualified Node-context profile first, then set
`nodeContext.topology.enabled=true`. The producer adds read-only mounts for
`/sys/devices/system`, `/sys/kernel/mm` and `nodeContext.topology.cgroupRoot`
(default `/sys/fs/cgroup`). Qualify that cgroup root for your runtime: the reader
uses only its HugeTLB files and never walks workload cgroups. Missing files at the
true root are reported as unsupported, not zero. Parent sysfs mounts allow missing
NUMA or HugeTLB child directories to be reported without failing Pod setup.

The profile retains the existing non-root user, dropped capabilities, read-only
container filesystem and network policy. It adds no BPF, SYS_ADMIN, host PID access,
host `/proc`, writable mount or hardware inventory. Rooted reads cannot follow
symlinks outside each configured root. Only memory-domain numbers and documented
memory values are retained; CPU sets, serial numbers, PCI identities and addresses
are not collected.

The separate `kube-memlens-node-topology-viewer` ClusterRole permits `get` on
`nodecontexts/topology` and is not bound automatically. Operators can create a
narrower role with `resourceNames` for specific Nodes. Existing Pod and Node viewer
roles do not acquire this permission. Every read passes the normal Kubernetes
resource authorisation path; ingestion uses the distinct Node producer identity.
The collector also requires fresh Node UID inventory before serving topology.

## Interpreting the evidence

- NUMA total/free values come from per-domain sysfs meminfo. Placement counters are
  cumulative pages, not access latency or measured remote-memory traffic.
- The informational uneven-distribution heuristic requires at least two
  memory-bearing domains, fresh consistent total/free observations, a spread
  strictly above 20 percentage points and normalised excess strictly above 64 MiB.
  Non-free memory includes cache, kernel memory and hugepage pools. Missing,
  changing, stale or single-domain topology does not establish balance or pressure.
- HugeTLB pool total already includes surplus pages. Free pages, outstanding
  reservations and current in-use pages are distinct. Inconsistent independently
  sampled fields remain partial and do not support a current-use calculation.
- Cgroup reservation-accounted bytes can include faulted allocations; they are not
  the global pool's outstanding, unfaulted reservation count. Finite limits are
  retained as reported byte values, even when not huge-page aligned.
- Cgroup `hugetlb.*.events` limit failures are cumulative. They do not establish
  global pool exhaustion, OOM kills or Kubernetes evictions.
- Kubelet availability may already account for hugepage capacity. KubeMemLens
  preserves it unchanged. The running HugepageAwareEviction gate and Topology
  Manager/Memory Manager policies are unreported; verify them in your Node's
  actual configuration before interpreting policy effects.

Collection shares the existing 15-second Node producer cycle, with a two-second
local work budget, no overlapping reads, eight NUMA domains, eight hugepage sizes,
8 KiB per file, 1 MiB aggregate reads, 384 file operations and 32 directory entries.
An observation is limited to 16 KiB. Source failures remain separate from retained
source timestamps; evidence becomes stale after 45 seconds. The collector retains
two bounded observations per existing Node ownership record, with no additional
history. At 5,000 configured Node records this adds at most about 156 MiB of encoded
source payload; choose the collector's Node bound and memory limit accordingly.

## Read and capture

The Node detail view has a separate scrollable topology section. Alongside the existing dashboard permissions, it requires the
optional profile and Node topology permission. The named API resource is
`/apis/memory.kubememlens.io/v1alpha1/nodecontexts/<name>/topology`, negotiated with
snapshot schema 7. Older schemas omit the producer component and cannot access
this new representation.

Capture explicitly:

```sh
kubectl memlens capture --node worker-a --include-topology --include-history -o node-topology.json
kubectl memlens replay node-topology.json
```

Schema 7 wraps the existing schema-4 Node document and retains topology source
values, original clocks, interpretations and caveats. Default captures remain
schema 4. Every export performs fresh authorised reads, including overwrite
attempts. By default, Node names become `node-1`, existing Node UID fingerprints
are retained, and NUMA domains receive deterministic local numeric aliases.
Those aliases cannot establish topology continuity across captures; schema-7
comparison is rejected. `--include-sensitive` preserves the original identifiers.
Files use the existing private, atomic capture writer and overwrite protection.

Disable `nodeContext.topology.enabled` to remove the mounts, optional endpoint and
viewer role. Ordinary Node data and captures keep their existing contract. Final
managed-provider qualification is tracked separately from local Linux validation.

See the [local Linux validation record](qualification-results/node-topology-local-linux-2026-09-29/README.md) and [trust boundary](security/node-topology.md).
