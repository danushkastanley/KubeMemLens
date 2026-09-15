# File/cache qualification workload

This fixture generates deterministic regular-file I/O for local BPF-005 tests.
It has no tracing code, Kubernetes access, network access or capability needs.
Keep it outside the standard agent, chart and release images.

`workload.c` owns only `/work/fixed-seed.bin`, an 8 MiB file containing a repeated
64 KiB block generated from xorshift32 seed `0x5eed1234`. Reads verify every byte.
Preparation refuses to replace an existing file; all other operations require a
regular, singly linked file owned by the current UID with exactly the fixed size.
The workload emits numerical totals and fixed mode names, never file contents.

| Mode | Operation | Required cache state |
| --- | --- | --- |
| `prepare` | Create, write and sync the fixed file | All pages resident afterwards |
| `cached` | Read and verify 8 MiB | All pages resident before and afterwards |
| `uncached` | Sync, advise DONTNEED on this file, then read and verify 8 MiB | Zero pages resident after advice; all resident after reading |
| `write` | Rewrite and sync 8 MiB | All pages resident afterwards |
| `noise` | Four complete write/read rounds, then sync | 32 MiB written and 32 MiB read |

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
timeout. It also checks rejection of replacement and corrupted bytes. It removes
its own container and volume, including on failure, and never overwrites evidence.
No trace or kernel programme is loaded by this verifier.

## Use during incident qualification

Run `prepare` once in each disposable target/noise Pod before observation starts.
Use a Pod `emptyDir` for `/work`, UID/GID/fsGroup 65532, dropped capabilities,
RuntimeDefault seccomp, a read-only root and explicit CPU/memory/volume ceilings.
The image's idle command exits after one hour; remove the owned Pods after tests.
Execute modes through bounded administrative `kubectl exec`, keeping the admitted
target container identity unchanged throughout each trace.

Start the selected operation only after the owned worker's attachments are
observed. For isolation, run `noise` concurrently in ten non-selected Pods across
two namespaces while the selected target is idle, then repeat with selected I/O.
Retain aggregate reference results and verify each captured owned BPF identity is
gone after the stream ends. A warm-read result does not imply a cache hit counter;
file-operation and page-add/remove observations remain different measurements.

Passing this fixture's verifier establishes the workload, not incident semantics,
privacy, isolation, resource ceilings, cancellation or teardown qualification.
Only an independently accepted incident worker may be loaded for those checks.

## Path-copy regression

`test_path_copy.py --compiler-image IMAGE_ID --output NEW_DIRECTORY` extracts the
producer's exact bounded copy helper and compiles a native test in the immutable,
capability-free compiler image. It models the kernel helper's backwards-built
path and prefix move, checks lengths from zero through 512 bytes and retains
guards around the destination. The former whole-buffer copy must fail the same
dirty-suffix check. This verifies byte-copy behaviour without loading BPF; it does
not replace verifier, permission-hook or live consented-path qualification.
