# ADR 0031: Keep the trace extension as a separately built extension

Date: 25 November 2026

Status: accepted by the maintainer on 25 November 2026; step 1 is implemented in the same pull request, steps 2 to 4 are not

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
   check that runs when those paths change and on a schedule.
3. **Park the confirmed-paths pipeline.** Until a shipped client can request
   confirmed paths, the pipeline is not extended and its tests leave the
   per-pull-request path with the harnesses in step 2. The code stays.
4. **Rename `prototype/trace`.** The directory and its two Go module paths
   are renamed to say what they are. The proposed name is `extensions/trace`.

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

## Not yet decided or verified

- The name in step 4. `extensions/trace` is a proposal.
- Exactly which tests count as the confirmed-paths pipeline in step 3. The
  audit's figure of about 1,370 lines has not been re-counted.
- How much time steps 2 and 3 take off the trace gate. On `main` at `fc11fb2`
  the whole gate took 493 seconds; the harness share was not measured.
- Whether to publish the extension later. This ADR does not decide it.

## Verification, migration and rollback

Step 1 is covered by a test that reads the real `--help` output. Steps 2 to 4
each bring their own verification in their own pull request. Nothing here
changes stored data or a wire format. Reverting step 1 restores the old help
text.
