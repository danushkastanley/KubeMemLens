# Agent Snapshot Stale

This alert means the collector has not received a fresh node snapshot for more than 30 seconds.

1. Run `kubectl memlens doctor --strict` and inspect node freshness, cgroup access, runtime layout, and mapping.
2. Check the agent Pod and logs on the affected node.
3. Verify the collector ingestion Service, NetworkPolicy enforcement, and agent-to-collector DNS/connectivity.
4. Confirm the node uses supported cgroup v2 and that `/sys/fs/cgroup` remains mounted read-only in the agent.

Do not use stale memory rows for incident decisions. Restore ingestion first and wait for a fresh snapshot.
