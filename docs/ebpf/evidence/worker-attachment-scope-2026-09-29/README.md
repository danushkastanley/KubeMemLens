# Constrained worker attachment scope — 29 September 2026

This records **non-loading** validation of the implementation in
[ADR 0020](../../../adr/0020-limit-worker-sdk-to-reviewed-attachments.md).
It is not a kernel, performance or R7-readiness result. No new worker was installed
or admitted by these checks; the standard product is unaffected.

The patch retains only the attachment classes already accepted by the signed
file/cache/OOM object policy. Malformed hooks, attachment overrides and other
programme classes are rejected. Discovery sources and upstream tests are intact;
the worker no longer links the container/network/TC/uprobe backend families.
Generic metadata and symbolisation helpers remain; no broader removal is claimed.

Native Linux arm64 tests passed with the actual cache, file and OOM objects, all
three prepared without readers or map loading. The attachment vocabulary tests
cover valid tracepoints and fentry/fexit, malformed fields, and rejected program
classes. Worker, filecache, installation and runtime race suites and vet passed;
optional runtime tests that require a separately signed installation fixture are
not represented as a kernel lifecycle pass. Launcher checks, Python contracts,
formatting and support/release/community contracts also passed.

A fresh native symbol-level govulncheck 1.7.0 scan against the 28 September
database reported no package or symbol findings. GO-2026-5932 remains a required-
module finding; it is retained, not suppressed. This is scoped to this worker
graph and is not a blanket statement about every module in the repository.

Both Linux architectures were built twice in the pinned, networkless release
builder. Each pair produced identical bytes and passed the dependency boundary:

| Architecture | Executable bytes | SHA-256 |
| --- | ---: | --- |
| arm64 | 18,874,492 | `ac227abc06ffd4923dad33d0e9450caf928a2e6eac7a96c7bbe38539792e1167` |
| amd64 | 20,226,172 | `2c4cd8d54a81cc6f1676fc42fb246e54116f002bb64bfc12f069ac0eabc3bf48` |

The previous admitted arm64 worker was 48,234,620 bytes. Size reduction does not
predict retained heap, BPF map charge, startup CPU or active working set. Sealed
execution and resource accounting are unchanged; the full matrix must use a new
matched immutable image, programme bundle and policy before any go decision.

[Summary and receipt hashes](summary.json) bind the SDK, patch, builder and
reproduced binaries. Private fixtures, signing material and workload identities
are not included. No EKS resources were created.
