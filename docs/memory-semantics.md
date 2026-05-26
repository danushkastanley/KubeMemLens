# Memory Semantics

Kubernetes memory incidents often start with a single number from `kubectl top`. That number is useful, but it does not explain what kind of memory is charged to the pod or container.

KubeMemLens starts from cgroup v2 memory files and separates the visible charge into smaller buckets.

## Total Charged Memory

`memory.current` is the total memory currently charged to the cgroup. It can include application memory, page cache, tmpfs, slab/kernel memory, and other accounted memory.

## Anon / RSS

`anon` is used as the RSS/anonymous memory proxy in this MVP. It often lines up with application heap, native allocations, runtime memory, and memory retained directly by the process.

High anon memory can suggest heap growth or native memory retention, but it is not proof of a leak by itself.

## File Cache

`file` is file-backed memory charged to the cgroup. This commonly includes page cache. A pod can look high-memory because it is reading or caching files heavily even when application heap is stable.

## Active File

`active_file` is file-backed memory the kernel considers active. A high value can suggest recently used page cache.

## Inactive File

`inactive_file` is file-backed memory the kernel considers less recently used. It may be reclaimable, depending on pressure and workload behaviour.

## Shmem / Tmpfs

`shmem` includes shared memory and tmpfs-backed memory. In Kubernetes this can include memory-backed `emptyDir`, `/dev/shm`, and workloads that write intermediate data to memory-backed filesystems.

## Slab / Kernel

`slab` is kernel memory charged to the cgroup. This can include dentries, inodes, socket-related memory, and other kernel-side allocations.

## Dirty / Writeback

`file_dirty` and `file_writeback` represent file-backed pages waiting to be written or actively being written back. Elevated values can suggest a write-heavy workload, slow writeback, or storage pressure.

## Memory Events

`memory.events` reports pressure and limit counters such as `high`, `max`, `oom`, and `oom_kill`. These counters are useful when deciding whether high memory is only noisy or has already caused pressure.

## Why High Pod Memory Is Not Always A Heap Leak

A pod can be charged for page cache, tmpfs, and kernel memory as well as application heap. KubeMemLens avoids saying "definitely a leak" and instead reports what the available cgroup stats suggest. The right next step depends on which bucket is high.
