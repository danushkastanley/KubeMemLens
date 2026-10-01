# Bounded verifier observation

This Linux qualification helper samples the numeric verifier transport once per
second for an explicitly owned cgroup. It does not load, attach or execute BPF
programmes. The separate `verifier-calibration` command validates positive and
negative fixed loads before campaign use.

Build from `prototype/trace` with an explicit output path:

```sh
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -o /private/output/verifier-observer ./qualification/verifier-observer
```

Run only on an approved disposable Linux node:

```sh
verifier-observer --config /private/input.json --acknowledge-owned-node
```

The configuration is a private regular file (no symlinks, at most 16 KiB):

```json
{
  "owner": "0123456789abcdef0123456789abcdef",
  "seconds": 10,
  "bootID": "00000000-0000-0000-0000-000000000000",
  "onlineCPUs": "0-3",
  "btfSHA256": "<actual kernel BTF SHA-256>",
  "anchor": {"pid": 2, "start": 123, "sha256": "<actual executable SHA-256>"},
  "group": {"path": "/sys/fs/cgroup/owned-fixture", "inode": 123}
}
```

All fields are required; replace the illustrative identities with measured ones.
Unknown, duplicate, null, trailing or incorrectly typed fields are rejected.
Choose a fresh 32-character lowercase hexadecimal owner and a non-root cgroup.
The complete online CPU roster must contain at most 64 CPUs, with IDs at most
4095. Duration is 1–1800 seconds. Pin the executable, process start time, cgroup
inode, kernel BTF and boot identity before invocation. The anchor must remain in
that exact cgroup for the entire observation.

The helper holds a pidfd, executable and cgroup descriptor. It checks process
liveness every 10 ms and revalidates executable identity, cgroup membership, boot
and topology for each row. Three exact owner-named probes use cgroup-filtered perf
descriptors, 64 KiB rings per CPU and bounded numeric decoding. It does not enable
global tracing or emit verifier log text, raw records or process identifiers.

Output contains `seconds + 1` JSON lines. Rows contain binding hashes, monotonic
cutoffs, numeric verifier totals, perf coverage, probe hit/miss counts and observer
CPU/peak RSS. The 100 ms ordering delay and at most 250 ms scheduling lateness are
explicit. Numeric reads must finish within 50 ms. The observer's resource cost is
reported separately from the target. A zero result is meaningful only after
positive calibration and complete coverage checks; an idle run alone does not
prove that verifier events can be observed.

Only the final row can have `captureClosed: true`. Its separate cleanup timestamps
follow the numeric read. Completion requires every captured record to be accounted
for, no lost samples or missed probes, no pending calls, and all owned resources
closed. A campaign must leave an idle tail: a call or record crossing the final
cutoff fails the observation instead of being silently omitted.

SIGINT, SIGTERM, owner exit and output errors return through cleanup. Broken stdout
is handled as an error rather than process termination by SIGPIPE. A hard deadline
of `seconds + 10` seconds bounds blocked output. SIGKILL or that hard deadline can
leave tracefs definitions: the outer controller must journal exact definitions
before execution, confirm the helper has exited, and remove only unchanged owned
definitions. Never truncate the tracefs registry. Retain incomplete streams and
cleanup failures; do not treat them as successful observations.

Tests cover strict configuration, native process ownership and bounded output.
Native runtime evidence, including forced-exit reconciliation, is required in
addition to these non-loading unit tests. This helper is not by itself evidence
of full active-campaign, provider or release qualification.
