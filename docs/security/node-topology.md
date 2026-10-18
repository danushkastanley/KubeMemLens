# Node topology trust boundary

The opt-in profile exposes bounded host memory topology to a distinct Node
producer and a named, separately authorised Node read. Ordinary Pod and Node
viewer roles receive no new permission. The standard chart has no new host mount.

The producer mounts fixed sysfs parents and a qualified cgroup root read-only.
Its adapter opens rooted handles, reads documented children, rejects traversal,
limits files/bytes/domains/page sizes, and never enumerates workload cgroups or
processes. Parent directories allow absent kernel interfaces to remain an explicit
unsupported source. A compromised producer could read other metadata within those
read-only parents; operators should enable this host visibility only on the Nodes
requiring topology context. The profile grants no capabilities, host PID namespace,
writable mount, network destination or scheduling authority.

Authenticated ingestion binds topology name, UID and report time to the separate
Node producer stream. Bounded strict decoding rejects unknown/duplicate fields,
source-clock replay and mismatched identities before the atomic store update.
Standard cgroup producers cannot submit this component. Data is retained within the
existing Node record bound; inventory replacement/removal revokes the record.
Named reads require fresh inventory in addition to Kubernetes resource access.

Captures require explicit opt-in and fresh authorised reads for each write or
overwrite. Default schema-7 captures pseudonymise Node identity and replace NUMA
numbers with local aliases. The original clocks and explanation caveats survive.
No CPU identifiers, serial numbers, PCI data, addresses or process names are
collected. Encoded source payloads and capture contents must not enter logs or
metrics. Disable the profile to remove host mounts and the optional read role.
