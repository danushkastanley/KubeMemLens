# Memory Semantics

Kubernetes memory incidents often start with a single number from `kubectl top`. That number is useful, but it does not explain what kind of memory is charged to the pod or container.

KubeMemLens starts from cgroup v2 memory files and presents one primary, non-overlapping composition plus explicitly overlapping secondary detail.

## Primary Composition

The compact CLI and TUI tables show:

- total charged memory;
- anonymous memory;
- filesystem cache excluding shmem;
- shmem/tmpfs; and
- residual/other.

The last four rows partition `memory.current`: `other = total - anon - (file - shmem) - shmem`. The independently sampled cgroup files can briefly disagree, so both subtractions floor at zero instead of wrapping. Residual/other includes kernel memory and any charge not represented by the first three buckets.

Kernel, slab, sockets, page tables, mapped-file, THP, active/inactive file, and dirty/writeback values are secondary evidence. They overlap the primary composition and must not be added to it.

## Total Charged Memory

`memory.current` is the total memory currently charged to the cgroup. It can include application memory, page cache, tmpfs, slab/kernel memory, and other accounted memory.

`memory.peak` preserves transient high-water usage that may no longer be visible in `memory.current`. `memory.min`, `memory.low`, `memory.high`, and `memory.max` describe protection and limit boundaries; `max` is represented explicitly as unlimited rather than converted to a byte value.

## Anon / RSS

`anon` is used as the RSS/anonymous memory proxy in this MVP. It often lines up with application heap, native allocations, runtime memory, and memory retained directly by the process.

High anon memory can suggest heap growth or native memory retention, but it is not proof of a leak by itself.

## File Cache

The raw cgroup `file` value includes shmem/tmpfs. KubeMemLens therefore displays filesystem cache as `file - shmem`, floored at zero, while retaining raw `file` in metrics and snapshots. A pod can look high-memory because it is reading or caching files heavily even when application heap is stable.

## Active File

`active_file` is file-backed memory the kernel considers active. A high value can suggest recently used page cache.

## Inactive File

`inactive_file` is file-backed memory the kernel considers less recently used. It may be reclaimable, depending on pressure and workload behaviour.

## Shmem / Tmpfs

`shmem` includes shared memory and tmpfs-backed memory. In Kubernetes this can include memory-backed `emptyDir`, `/dev/shm`, and workloads that write intermediate data to memory-backed filesystems.

## Kernel Detail

Raw `kernel` includes slab. KubeMemLens exposes total kernel, total slab, slab reclaimable/unreclaimable, socket memory, page tables (including secondary page tables where the kernel reports them), and `kernel - slab` as overlapping secondary detail. These values help distinguish reclaimable caches from unreclaimable or workload-facing kernel charges; they do not form another additive partition of total memory.

Mapped-file and transparent huge page counters (`file_mapped`, `anon_thp`, `file_thp`, and `shmem_thp`) are also retained as overlapping secondary evidence.

## Pressure, events, and swap

`memory.pressure` provides PSI `some` and `full` rolling averages and cumulative stall time. A recent `memory.events.local:high` delta means the local cgroup crossed `memory.high` and its processes were routed into direct reclaim. KubeMemLens prefers local event deltas for leaf-container diagnosis while retaining the hierarchical `memory.events` counters.

`memory.swap.current`, `memory.swap.peak`, `memory.swap.max`, and swap-event deltas show whether anonymous memory is being swapped or swap allocation is being throttled or rejected. Reclaim/refault counters from `memory.stat` are collected as evidence; they are not presented as root cause without a time-window comparison.

The collector records the previous node-snapshot timestamp only when the same container ID is observed again. Explanations therefore keep the instantaneous gauge timestamp separate from the exact counter-delta start/end. First observations and replaced containers explicitly report an unavailable or incomplete delta window; multi-node workload roll-ups expose outer bounds and report whether their per-container windows are uniform. This prevents a cumulative counter or mixed sampling interval from being presented as a single recent event window.

## Dirty / Writeback

`file_dirty` and `file_writeback` represent file-backed pages waiting to be written or actively being written back. They overlap file-backed memory. Elevated values can suggest a write-heavy workload, slow writeback, or storage pressure.

## Memory Events

`memory.events` reports pressure and limit counters such as `high`, `max`, `oom`, and `oom_kill`. These counters are useful when deciding whether high memory is only noisy or has already caused pressure.

## Why High Pod Memory Is Not Always A Heap Leak

A pod can be charged for page cache, tmpfs, and kernel memory as well as application heap. KubeMemLens avoids saying "definitely a leak" and instead reports what the available cgroup stats suggest. The right next step depends on which bucket is high.
