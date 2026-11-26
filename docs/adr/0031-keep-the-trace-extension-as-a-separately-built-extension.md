# ADR 0031: Keep the trace extension as a separately built extension

Date: 25 November 2026

Status: accepted by the maintainer on 25 November 2026; step 1 is implemented in the same pull request and step 2 in a later one; step 3 is re-counted below and awaits a decision; step 4 is implemented in a later one

## Context

The November 2026 audit (decision D-1, issue 304) asked what to do with the
trace area. It found:

- `prototype/trace` is about 27,800 lines of non-test Go, roughly a third of
  the repository, and holds 38% of its test Go.
- It is the only server behind the six `kubectl memlens trace` commands, which
  ship in every release of the CLI. It is in no release artefact, so on a
  standard install those commands can only report that the optional trace
  service is unavailable. Nothing in `--help` said so.
- The directory is called `prototype`, although it is the real server and not
  an experiment that a later implementation will replace.
- Qualification harnesses for a candidate that ADR 0015 rejected are compiled
  and tested on every pull request.
- About 1,370 test lines cover a confirmed-paths pipeline that no shipped
  client can request.

The audit offered three directions: publish the extension, keep it as an
unpublished development extension, or park it.

## Decision

Keep it. The trace extension stays in the repository as an optional extension
that is built and installed separately, and is not added to the released chart
or image by this decision. Four changes follow from that, in this order.

1. **Say so in `--help`.** `kubectl memlens trace --help` and the command list
   state that the extension is separately built and is not part of the
   released chart or image. Done in the pull request that adds this ADR.
2. **Take the qualification harnesses off the per-pull-request path.** The
   Go packages under `prototype/trace/qualification` and the Python tests
   under `hack/ebpf-qualification` and `hack/ebpf-active-qualification` move
   from `make check-trace-preflight`, which gates every pull request, to a
   check that runs when those paths change and on a schedule. Done: see
   [Step 2 as implemented](#step-2-as-implemented).
3. **Park the confirmed-paths pipeline.** Until a shipped client can request
   confirmed paths, the pipeline is not extended and its tests leave the
   per-pull-request path with the harnesses in step 2. The code stays. Not
   moved yet: the re-count in
   [Confirmed-paths tests](#confirmed-paths-tests) found far fewer
   pipeline-only tests than the audit's figure.
4. **Rename `prototype/trace`.** The directory and its two Go module paths
   are renamed to say what they are. Done: the maintainer chose
   `extensions/trace`. The directory is now `extensions/trace`, and the module
   paths are `github.com/danushkastanley/kube-memlens/extensions/trace` and
   `github.com/danushkastanley/kube-memlens/extensions/trace/worker`. Paths
   under `prototype/trace` elsewhere in this ADR record the tree as it was
   before the rename; each now lives at the same place under
   `extensions/trace`.

## Why this order

- Step 1 is small, changes no behaviour and fixes what a user sees today.
- Steps 2 and 3 change which checks gate a merge. `docs/verification.md` asks
  for that to be done deliberately: each needs its own pull request, with the
  list of checks that stop gating and where they run instead.
- Step 4 touches every import of the two modules, the Dockerfiles, four
  workflows, the Dependabot configuration and the documents. It should be one
  mechanical pull request with no other change in it, opened when no other
  pull request touches the tree, because it conflicts with all of them.
  Dependabot pull requests against `prototype/trace` are open now.

## Alternatives considered

- **Publish the extension.** It would make the `trace` commands work on a
  standard install. Rejected for now: the extension loads eBPF programmes with
  elevated privileges, its resource qualification is incomplete, and ADR 0002
  requires external qualification before a public release.
- **Park the whole area.** Removes a third of the repository from every
  change. Rejected: the six `trace` commands would then have no server at all,
  and the audit's five trace defects have since been fixed (issues 288 to 292).
- **Hide the `trace` commands.** Fewer confused users, but the commands are
  how the extension is exercised, and hiding them does not explain why they
  fail.
- **Rename first.** It is the most visible change, but doing it while other
  pull requests touch the tree costs every one of them a conflict.

## Consequences

- A user who runs a `trace` command on a standard install is told beforehand,
  in help, that it needs something they have not installed.
- After step 2 a change that breaks a qualification harness is found when
  that harness's paths change or on the scheduled run, not on every pull
  request.
- After step 3 the confirmed-paths code can drift from the rest of the
  extension without a per-pull-request test noticing. That is the cost of
  parking it, accepted until a client exists.
- After step 4 every external reference to `prototype/trace` breaks: saved
  commands, documents outside the repository, and the Go import paths.

## Step 2 as implemented

- `make check-trace-qualification` runs the tests that left
  `make check-trace-preflight`: the Go packages below
  `prototype/trace/qualification/` with the race detector, and the Python
  tests under `prototype/trace/qualification/isolation`,
  `prototype/trace/qualification/filecache` (the `*observation` and `*series`
  patterns), `hack/ebpf-qualification` and `hack/ebpf-active-qualification`.
  `make check` runs it.
- `.github/workflows/trace-qualification.yml` runs it when those paths, the
  module's `go.mod` or `go.sum`, the Makefile, `hack/trace_packages.py` or the
  workflow change, weekly, and on demand. It is not a required status.
- `make check-trace-preflight` still verifies the module, vets, scans and
  cross-builds every package, harnesses included, so a harness that stops
  compiling still fails the required "Go checks".
- `hack/trace_packages.py` derives both package lists from one `go list ./...`
  and fails when they do not partition the module or disagree with
  `go list ./qualification/...`, so a new package can only leave the
  per-pull-request gate by being placed below `qualification/`.
- Two tests under `qualification` stay on the per-pull-request gate, because
  they check shipped files rather than a harness: the package at
  `prototype/trace/qualification` itself (its test checks the
  `seccomp/preflight.json` profile the trace chart installs), and
  `qualification/filecache/test_pending_path.py` (it compiles functions from
  `programmes/filecache/files.bpf.c`).
- Before and after lists of test functions and Python test IDs were compared
  and are identical: 289 Go tests on macOS from `go test -list`, 437 on Linux
  read from the Linux test files, and 413 Python tests. Of the Linux Go tests,
  279 stay per pull request and 158 moved; of the Python tests, 13 stay and
  400 moved.
- Measured on a maintainer's macOS machine with a warm build cache and the Go
  test cache disabled, the trace gate took 62 and 46 seconds before and 23 and
  22 seconds after; the new check took 35 and 23 seconds. With the test cache
  warm the figures were 30 and 32, then 16 and 14, and 16 and 17 seconds. CI
  time on Linux, where more harness packages build, was not measured.

## Confirmed-paths tests

No shipped client can request confirmed paths. `kubectl memlens trace` and
the TUI build their request with `traceclient.DefaultIntent`, which sets
`trace.OmitPaths`, and no other code in `internal/` or `cmd/` changes it.
`internal/traceclient/selection.go` sends `rawPaths` only when the intent asks
for `trace.ConfirmedPaths`. Outside tests, only the qualification clients in
`prototype/trace/qualification/isolationclient` and `delivery-client` ask for
it, and they moved with step 2.

Tests that exercise only the confirmed-paths pipeline, re-counted on
26 November 2026 (function lines):

| Test | Lines | Runs in |
| --- | --- | --- |
| `prototype/trace/workeripc/protocol_test.go` `TestConfirmedPathsRemainExactOnlyInExplicitPipe` | 28 | trace gate |
| `prototype/trace/filecache/records_test.go` `TestOnlyInvalidUTF8PathIsUnrepresentable` | 9 | trace gate |
| `prototype/trace/worker/sdk/read_linux_test.go` `TestNonUTF8PathIsRejectedWithoutEndingTrace` and its helper `confirmedFileRecord` | 33 | worker gate |
| `internal/tracesession/file_batch_test.go`: four `TestFileBatch*` tests and `TestFailedFileBatchCannotEmitTerminalSummary`, not `TestFileBatchOmitPathsAndClosedOutput` | 99 | Go tests |
| `internal/tracesession/aggregate_test.go` `TestV2ConfirmedPathsKeepEventsAndAggregateCountsAligned`, `TestV2PartialRawWriteCannotClaimAggregateDelivery` | 29 | Go tests |
| `internal/traceframe/benchmark_test.go`: two benchmarks and their helper | 68 | compiled only |

That is 266 lines, against the audit's estimate of about 1,370. The
difference is the batching tests: `admissionapi/relay_batch*_test.go` and
`workeripc/file_batch*_test.go` and `reader_*_test.go` (826 lines) build their
fixtures with confirmed paths, but the code they test is shared. The relay
batches every event frame, including the OOM event frames a shipped
`trace --kind oom` receives, and the worker pipe carries every file activity,
including those of a default trace that only reports aggregates. Those tests
stay on the per-pull-request path, as do tests that check both policies or the
guard that refuses confirmed paths without an accepted installation.

## Not yet decided or verified

- What step 3 moves, given the re-count. Moving the 70 pipeline-only lines in
  the two trace modules saves no measurable time. The 128 test lines in
  `internal/` run in the main module's "Go tests", which would need its own
  change to move them.
- How much time steps 2 and 3 take off the trace gate in CI. On `main` at
  `fc11fb2` the whole gate took 493 seconds; only local times were measured
  for step 2.
- Whether to publish the extension later. This ADR does not decide it.

## Verification, migration and rollback

Step 1 is covered by a test that reads the real `--help` output. Steps 2 to 4
each bring their own verification in their own pull request. Nothing here
changes stored data or a wire format. Reverting step 1 restores the old help
text. Reverting step 2 puts the harness tests back in the trace gate. Reverting step
4 restores the old directory and module paths; it changes no behaviour.
