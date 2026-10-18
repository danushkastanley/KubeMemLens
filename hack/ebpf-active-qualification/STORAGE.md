# Storage sizing observations

`storage_probe.StorageProbe` is a read-only before/after probe for already-bound
selected and noisy fixture Pods. It neither creates workloads nor changes
cgroups, cache state, storage placement, limits or block-device settings. It is
separate from the frozen performance windows; source/unit tests are not live
storage or EKS qualification evidence.

`storage_sizing.py` supplies the bounded controller around the probe. It uses one
complete 1,350-second untraced control window from the unchanged noisy profile:
32 fixture containers, the selected cached workload, ten noisy neighbours and
1,320 fixed one-second operations per active workload. It first verifies that
the owned optional services are idle, stops them, checks their previous processes
exited, and retains the 60-second settling period. The ordinary control Window
checks absence at both ends and records resource/service observations throughout.

```sh
python3 hack/ebpf-active-qualification/storage_sizing.py \
  --config /private/approved-active.json \
  --output /private/new-storage-evidence \
  --acknowledge-local-storage-sizing
```

For an approved EKS host, explicitly substitute
`--acknowledge-owned-eks-storage-sizing`. The existing runtime verification,
configuration/source/helper binding and full certificate checks remain. There
is no count or duration override. A 3,000-second alarm bounds the controller;
the outer cloud controller still owns infrastructure expiry and teardown.

The controller retains before/after snapshots and exact selected/noisy bindings,
then independently validates the control envelope, all eleven application
streams, full resource and standard-service streams, identities and density. The
comparison records application bytes/operation latency and CPU throttling deltas
separately from device rates. Snapshot intervals also include the intervening
setup and closing checks, so rates use those conservative time bounds. The final
completed sizing receipt is written only after original services are restored
and owned fixtures are removed. Failures retain partial evidence and cannot
produce that receipt. This one control window is not a paired qualification case.

The caller supplies a verified Case and at most twelve unique owned fixture
bindings. The probe rechecks their Pod, process, executable, CRI and cgroup
identities before and after each snapshot. It records the exact `/work` mount
from that process's mount inventory, cgroup `io.stat`, CPU accounting and I/O
pressure. It reads backing block statistics, resolves partition parents, and
checks sysfs device inode stability. At most sixteen device records are allowed;
each counter source is bounded to 64 KiB and runtime commands have timeouts.

Save snapshots as private evidence: they contain fixture names, mount roots,
device identities and raw mount inventories. Bind them to the frozen controller,
workload source, configuration and completed application-operation records. The
same probe instance must produce both snapshots. Neither the probe nor its
comparison function establishes that tracing was off, that the planned workload
completed, or that a volume was provisioned with a particular EBS performance
limit; the surrounding sizing controller must verify those facts separately.

`storage_pair.compare_storage` independently decodes the retained raw records,
checks fixture/mount/boot/observer/device identities and rejects counter resets.
It computes conservative integer rate bounds from the uncertainty intervals
around reads. Empty, absent or changing accounting stays unproven. Explicitly
observed zero counters remain zero. Extra I/O latency/depth fields are retained
but not subtracted as byte or operation counters.

Cgroup counters and device counters serve different purposes: the former report
per-cgroup I/O accounting; the latter include other activity on the same device.
Do not add a partition's counters to its parent's counters. Do not treat the
in-flight gauge as a cumulative count or infer an application latency percentile
from block timing totals. CPU/pressure raw records need separate interpretation;
this probe does not declare throttling absent.

Use completed application operations and their fsync/operation latency alongside
these observations when comparing sustained demand and headroom with both the
instance's baseline and the volume's provisioned throughput/IOPS. Do not infer
physical demand from application byte counts alone. The probe never returns a
capacity or qualification pass, and live validation is still required.

References: [Linux cgroup v2 I/O accounting](https://docs.kernel.org/admin-guide/cgroup-v2.html#io)
and [block statistics and 512-byte sector units](https://docs.kernel.org/block/stat.html).
