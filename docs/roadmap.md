# Roadmap

## v0.1: Local Parser And Sample CLI

- Parse cgroup v2 memory files.
- Model RSS/anon, file cache, tmpfs, slab, dirty/writeback, and pressure events.
- Explain local sample data.
- Keep Kubernetes mode as honest placeholders.

## v0.2: Kubernetes Cgroup Mapping And Collector Snapshots

- Walk container cgroups on each node.
- Map container IDs to pod/container metadata.
- Post agent snapshots to an in-memory collector.
- Add collector-backed `top pods`, `top ns`, and `explain pod` commands.

## v0.3: TUI

- Add a Bubble Tea terminal dashboard.
- Show pod/container memory buckets and diagnosis.
- Keep the interface terminal-first and incident-friendly.

## v0.4: Prometheus/OpenMetrics Export

- Expose recent memory bucket metrics.
- Keep labels controlled to avoid cardinality surprises.
- Document scrape and retention expectations.

## v0.5: Optional eBPF File Attribution

- Attribute file-cache behaviour more precisely where safe.
- Keep eBPF optional and separately reviewed.
- If/when eBPF programs are added, the repository may need a separate license strategy for BPF code depending on helper usage.

## v0.6: On-Demand Pod Tracing

- Add short-lived, on-demand tracing for selected pods.
- Avoid always-on deep tracing unless explicitly enabled.
- Keep storage local and bounded by default.
