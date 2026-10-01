# File/cache qualification workload

This fixture generates deterministic regular-file I/O for local BPF-005 tests.
It has no tracing code, Kubernetes access, network access or capability needs.
Keep it outside the standard agent, chart and release images.

`workload.c` owns only `/work/fixed-seed.bin`, an 8 MiB file containing a repeated
64 KiB block generated from xorshift32 seed `0x5eed1234`. Reads verify every byte.
Preparation refuses to replace an existing file; all other operations require a
regular, singly linked file owned by the current UID with exactly the fixed size.
The workload emits numerical totals and fixed mode names, never file contents.
Timed series also record monotonic start/end times and elapsed nanoseconds for the complete
operation after opening the verified fixture: cache advice/residency checks,
transfers, required sync and close. It excludes process startup, seed generation,
opening and JSON output. Compare only identical modes against their paired control;
this elapsed time is not event-delivery latency. Ordinary modes have a ten-second
process alarm; inconsistent or missing series timing evidence fails verification.

| Mode | Operation | Required cache state |
| --- | --- | --- |
| `idle` | Wait at most 30 minutes without opening the fixture or emitting output | None |
| `prepare` | Create, write and sync the fixed file | All pages resident afterwards |
| `cached` | Read and verify 8 MiB | All pages resident before and afterwards |
| `uncached` | Sync, advise DONTNEED on this file, then read and verify 8 MiB | Zero pages resident after advice; all resident after reading |
| `write` | Rewrite and sync 8 MiB | All pages resident afterwards |
| `noise` | Four complete write/read rounds, then sync | 32 MiB written and 32 MiB read |
| `paired-idle` | Hold a paired benchmark fixture for at most one hour | No fixture I/O |

`cached --gated` and `uncached --gated` open and validate the file, emit
`{"ready":true}`, then wait up to 30 seconds for the single stdin byte `R`.
They perform the operation, emit its usual numerical receipt, and wait up to
30 seconds for `Q` before exiting. EOF, another byte or a timeout fails explicitly.
An overall 70-second alarm bounds both waits and the operation.
`gated.py` controls this handshake with bounded reads. Starting the process before
attachment and keeping it alive through teardown excludes CRI runtime setup and
exit I/O from an exact selected-workload measurement; no events are subtracted.

Residency is measured with `mincore` over a temporary, non-faulting mapping of
the owned file. Advice that leaves any page resident is an explicit failure.
The fixture does not write `drop_caches`, advise another file, or claim that a
device's own cache is cold. Residency and I/O totals are reference observations;
they do not by themselves prove BPF attribution or cgroup charge ownership.

## Build and verify locally

Use an inspected Linux compiler image with GCC and static libc, identified by
an immutable local image ID. The local arm64 verification used the existing
Linux race-test toolchain image recorded in private qualification evidence.
Set `IO_COMPILER_IMAGE` to that image ID and `IO_BUILD_DIR` to a new absolute
directory. Do not point either mount at private signing or cluster credentials.

From this directory:

```sh
mkdir -m 700 "$IO_BUILD_DIR"
mkdir -m 700 "$IO_BUILD_DIR/work"
docker run --rm --network=none --cap-drop=ALL \
  --user "$(id -u):$(id -g)" \
  --security-opt=no-new-privileges --read-only --cpus=2 --memory=512m \
  --pids-limit=64 --tmpfs /tmp:rw,nosuid,noexec,size=128m \
  --mount "type=bind,source=$PWD/workload.c,target=/workload.c,readonly" \
  --mount "type=bind,source=$IO_BUILD_DIR,target=/output" \
  "$IO_COMPILER_IMAGE" sh -ec '
    for n in 1 2; do
      gcc -std=c11 -Wall -Wextra -Werror -O2 -static \
        -fstack-protector-strong -U_FORTIFY_SOURCE -D_FORTIFY_SOURCE=3 \
        -Wl,--build-id=none -o /output/workload-$n /workload.c
    done
    cmp /output/workload-1 /output/workload-2
    cp /output/workload-1 /output/workload
  '
docker build --network=none -f Dockerfile -t kml-filecache-workload:local "$IO_BUILD_DIR"
docker image inspect kml-filecache-workload:local --format '{{.Id}}'
```

Pass the printed immutable image ID to `verify_workload.py --image IMAGE_ID
--output NEW_EVIDENCE_FILE`. The verifier creates a uniquely named Docker volume
and runs every mode with no capabilities or network, a read-only root, UID 65532,
one CPU, 64 MiB memory and at most 16 processes. Each invocation has a 15-second
timeout. It also checks idle startup without file creation or output, gated
read/residency receipts and process retention, plus rejection of invalid commands,
replacement and corrupted bytes. It removes
its own container and volume, including on failure, and never overwrites evidence.
No trace or kernel programme is loaded by this verifier.

## Use during incident qualification

Run `prepare` once in each disposable target/noise Pod before observation starts.
Use a Pod `emptyDir` for `/work`, UID/GID/fsGroup 65532, dropped capabilities,
RuntimeDefault seccomp, a read-only root and explicit CPU/memory/volume ceilings.
The image's `idle` command exits after 30 minutes; remove the owned Pods after tests.
The active benchmark uses the separate `paired-idle` command, bounded to one hour,
because its control and enabled windows share the same fixture lifetime. This
does not extend the ordinary isolation fixture or any trace admission deadline.
Freeze and verify the Pod UID, full CRI container ID, process lifetime, node and
workload executable hash. Run bounded administrative CRI exec against that exact
container ID, keeping the admitted identity unchanged throughout each trace.
For exact selected byte/page totals, start the gated process and receive readiness
before attaching, send `R` after the kernel witness, and send `Q` after cleanup.

Start the selected operation only after the owned worker's attachments are
observed. For file isolation, run `noise` concurrently in ten non-selected Pods
across two namespaces while the selected target is idle, then repeat with selected
I/O. For cache isolation, repeat `uncached` on those peers and verify residency;
warm file I/O cannot prove isolation from page-add/remove events.
Retain aggregate reference results and verify each captured owned BPF identity is
gone after the stream ends. A warm-read result does not imply a cache hit counter;
file-operation and page-add/remove observations remain different measurements.

Passing this fixture's verifier establishes the workload, not incident semantics,
privacy, isolation, resource ceilings, cancellation or teardown qualification.
Only an independently accepted incident worker may be loaded for those checks.

## Persistent operation series

`series MODE COUNT PERIOD_MS` runs one process with absolute monotonic deadlines.
Only `cached`, `uncached`, `write` and `noise` are accepted. Counts are bounded to
1–1,800, periods to 100–10,000 ms and total scheduled time to 30 minutes. Prepare
the owned fixture first. The first operation is due five seconds after a bounded
wall/monotonic clock alignment reading. The initial record exposes that alignment
and schedule; later records contain the operation index, deadline, I/O evidence
and three monotonic timing fields. Ordinary and gated operations retain their
exact seven-field I/O receipt; timing fields belong only to series records.

Operations must finish before their next deadline. A late operation fails the
series rather than shifting the remaining schedule or omitting evidence. A process
alarm bounds the series plus reporting time. Records are buffered in a fixed-size
array until the schedule ends, avoiding traced reporting I/O during operations.
The complete result is required; a start record alone cannot establish success.

`verify_series.py` rejects missing, reordered, duplicate, late or malformed
observations, mismatched schedules and unexpected fields. `verify_workload.py`
exercises all four short series and argument ceilings in the same restricted
container profile, then removes its owned resources. Run the parser regressions:

```sh
python3 -m unittest discover -s . -p 'test_*observation.py'
python3 -m unittest discover -s . -p test_series.py
python3 -m unittest discover -s . -p test_mixed_series.py
```

The scheduled byte rate is controlled by the test. Report achieved operations and
latencies against the identical paired schedule; do not describe a paced series as
maximum storage throughput. Startup, deadlines and every failed run remain part of
the qualification record. This fixture does not extend admitted trace durations.

`mixed-series COUNT PERIOD_MS` uses a separate schema-2 `mixed-series-start`
record and repeats cached read, uncached read, then write in that exact order.
Each operation retains the same cache-state, integrity, byte-count and timing
checks. The count must be a multiple of three between 3 and 18,000; periods remain
100–10,000 ms and the complete schedule is bounded to 30 minutes. This permits a
sustained 10 Hz workload beyond the uniform series' 1,800-operation limit. All
records remain buffered until the schedule ends; complete output is bounded to
8 MiB by `validate_mixed_series`. The existing schema-1 series and seven-field
ordinary/gated receipts are unchanged. The native verifier checks a 30-operation
mixed cycle and rejects invalid counts, periods and total durations.

A missed deadline flushes the completed operations and the late operation before
exiting with failure. This partial stream remains invalid for comparison; no slot
is retried or moved. A final `series-deadline-failure` record binds the same
sequence to monotonic wait/wakeup times and cumulative process CPU readings around
the wait and operation. This separates late wakeup from seed/open/stat setup and
the existing timed I/O span. CPU deltas include the clock probes at their sampling
boundaries; these values alone cannot attribute a delay to quotas or the host.
Successful series and ordinary/gated receipt formats remain unchanged.
The native verifier deliberately pauses only its owned
container across a deadline, then verifies both the retained timing evidence and
the failed result. Other fatal I/O failures may leave only the start record.

A campaign must freeze its mixed schedule separately and compare each operation
class against the same paired indices. An aggregate percentile can conceal a
regression in one class. This generator alone does not establish sustained traced
performance, isolation, flood behaviour or a qualification verdict.

## Path-copy regression

`test_path_copy.py --compiler-image IMAGE_ID --output NEW_DIRECTORY` extracts the
producer's exact bounded copy helper and compiles a native test in the immutable,
capability-free compiler image. It models the kernel helper's backwards-built
path and prefix move, checks lengths from zero through 512 bytes and retains
guards around the destination. The former whole-buffer copy must fail the same
dirty-suffix check. This verifies byte-copy behaviour without loading BPF; it does
not replace verifier, permission-hook or live consented-path qualification.

## Finite flood workload

`flood COUNT` opens the existing owned 8 MiB fixture read-only and requires it to
be fully cached. It emits the existing readiness record, waits for `R`, then
performs exactly COUNT successful 64-byte reads with fixed-seed integrity checks.
COUNT is bounded to 1–262,144; wrapping the file seeks to its start. A partial or
interrupted read fails instead of silently changing the syscall count. The burst
must complete within ten seconds, and the process retains the bounded gated
lifetime and final `Q` handshake. It neither creates nor changes file content.

The separate flood receipt contains schema version, mode, file size, read-call
count, bytes per read, total bytes and monotonic operation timing. `verify_flood.py`
validates those independent workload counters. They do not establish how many
BPF events were produced, lost or delivered: the controller must match readiness,
actual admission, event/output outcome, resource limits and owned cleanup.
The initial controller burst will use 131,072 reads (8 MiB total application I/O).
