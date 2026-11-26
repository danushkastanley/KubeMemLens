# ADR 0029: Tolerate bounded clock skew between the trace controller and a node

Date: 25 November 2026

Status: proposed for review; implemented in the same pull request

## Context

The trace controller and each node exchange absolute wall-clock instants: the
expiry of a pending binding and the deadline of a stream. Each side compares
them with its own clock and allows no difference. The only slack is the time
the request spends in transit. Issue 289 (audit T-02) reports two of these
comparisons. Reading and running the code shows five, and they are all on the
path of one trace:

| Where | Comparison | Result when the node clock is behind |
| --- | --- | --- |
| `extensions/trace/nodebinding/service.go`, `bind` | time to `expires` must be at most `maxLease` (15 s) | 409 `target changed` |
| `extensions/trace/nodebinding/stream.go`, before activation | frame metadata requires `deadline` at most session start plus the admitted duration | 503 `unavailable` |
| `extensions/trace/nodebinding/activation.go`, `activate` | `deadline` at most now plus the admitted duration | 409 `target changed` |
| `internal/tracesession/session.go` | the session deadline is the earlier of start plus duration and the stream deadline | the frame carries the node's shorter deadline |
| `internal/traceframe/match.go`, on the controller | the frame deadline must equal the controller's lease deadline | `target changed` |

The default pending lifetime is 15 seconds, the same as `maxLease`, and a
stream deadline is the controller's clock plus the full admitted duration. So a
node whose clock is behind the controller's by more than the transit time fails
every bind, and would fail every stream.

Reproduced on `main` at `96ae0dd` with a throwaway test that shifts the
controller's instants 200 ms into the node's future, which is what a node 200 ms
behind sees:

- `bind` with a full pending lifetime: `trace target changed`.
- A stream with the full admitted duration: `trace admission unavailable`.
- `activate` called on its own: `trace target changed`.

Neither category tells the operator that a clock is wrong, and no document
states that the clocks must agree.

A node whose clock is ahead behaves differently. It sees less time than the
controller granted, so the pending binding and the trace are shorter by the
difference. Once nothing is left, `bind` returns `target changed`.

One rule constrains the fix. `internal/traceframe/match.go` states: "The node may choose
its session start, but cannot replace identity, policy or deadline." The exact
deadline match exists so that a node cannot run a trace for longer than the
controller admitted.

## Decision proposed

Allow a fixed difference of one second between the two clocks, and keep every
real-time bound as it is.

1. One constant, the skew allowance, of one second. It is not configurable.
2. `bind`: accept a time to `expires` of up to `maxLease` plus the allowance.
   Store the lease with an expiry no later than the node's now plus `maxLease`,
   so a pending binding is never held for longer than today.
   Keep the replay record until the instant the controller sent, because the
   request stays acceptable until then.
3. Stream and `activate`: accept a `deadline` of up to now plus the admitted
   duration plus the allowance. Run with the earlier of that deadline and the
   node's now plus the admitted duration. That shortened deadline is the one
   used for the frame metadata, the execution and the worker. By its own clock
   the node never observes for longer than the admitted duration.
4. Controller match: accept a frame deadline that is not after the lease
   deadline and not more than the allowance before it. The node still cannot
   extend the deadline. It can shorten it by at most one second. Identity,
   policy, bounds and digests still have to match exactly.
5. Beyond the allowance the node refuses with `unavailable`, not
   `target changed`, and records a `clock_skew` audit event. The caller cannot
   fix a node clock, so this is an installation fault, and the audit event is
   how the operator learns the cause. No new public error category is added.
6. A node that finds an instant already in its past returns `expired`, not
   `target changed`.
7. The trace installation and operations documents state that controller and
   node clocks must agree to within one second, and name the audit event.

Timestamps in a stream stay in the node's clock, as the session start already
is. With this change they can differ from the controller's clock by up to one
second.

## Alternatives considered

- **Add the allowance to the two comparisons named in the issue.** Not enough:
  the stream then fails at the frame metadata check, and after that at the
  controller's exact match.
- **Send relative lifetimes instead of instants.** Removes the comparison
  between clocks, but changes both private wire formats and every check built
  on the absolute deadline, including the worker protocol's natural-expiry rule.
  A much larger change for the same result.
- **Keep the exact rule; fix only the category and the documentation.** Small,
  but an ordinary cluster does not keep clocks within a few milliseconds, so the
  feature would still fail on a node that is slightly behind.
- **A new public error category for clock skew.** Tells the caller more, but
  adds a status to the node protocol and to the public admission API for a
  fault the caller cannot act on.
- **A configurable allowance.** Another setting to validate and document. One
  second is far looser than a synchronised cluster needs and far tighter than a
  node that should not be trusted with timestamps.

## Consequences

- The rule "the node cannot replace the deadline" becomes "the node cannot
  extend the deadline, and can shorten it by at most the allowance". Documents
  that state the exact rule must change with the code.
- A stream's reported deadline can be up to one second earlier than the
  controller's lease deadline. The controller's own timers do not change: it
  still stops waiting two seconds after its lease deadline.
- A pending binding and a trace on a node that is behind last as long, in real
  time, as on a node with a correct clock. Today they fail.
- A node that is ahead still gets a shorter trace, by at most the difference.
  This ADR does not change that.
- The controller, the node service and the frame matcher change together. The
  node and controller are released as one version, so no mixed-version
  behaviour is promised. It should still be stated in the release notes.

## Not yet verified

These are inferred from reading and must be shown by running before the
implementation merges:

- That no other check on the path compares a node instant with the
  controller's clock. Candidates not yet read in full: the audit records in
  `internal/traceaudit`, the public admission API's handling of the lease
  deadline, and the client view of the stream metadata.
- That the worker request stays valid when the node shortens the deadline. It
  requires the deadline to be at most the admitted duration after the issue
  time, and both instants then come from the node.
- That the supervisor still recognises natural expiry, which needs the
  context's deadline to equal the request deadline exactly.
- Behaviour on a real cluster with two machines whose clocks differ. Everything
  here was run in one process on one clock.

## What implementing it showed

- `activate` needed no change. The stream handler shortens the deadline before
  it builds the frame metadata, and `activate` then receives an instant that is
  already within the admitted duration. Its own check, and the test that a node
  cannot extend beyond the admitted duration, are unchanged.
- No other comparison between a node instant and the controller's clock was
  found. `internal/traceaudit` compares no deadlines. The controller's lease
  compares only its own deadline with its own clock. The frame reader checks
  events and the summary against the metadata the node sent.
- The worker request and the natural-expiry rule were checked by reading only.
  `workerruntime` takes the request deadline from the context it is given, and
  the session sets that context's deadline to the shortened one, so the two
  stay equal. No worker was run with a shortened deadline.
- The public admission API's stream route was not run with a lagging node. It
  calls the same match, which is covered by the property test.

## Verification, migration and rollback

Acceptance for the implementation:

- A `nodebinding` test in which the node sees the controller's instants 200 ms
  in its future: `bind` succeeds, and a stream runs to natural expiry and
  delivers its terminal summary through the real TLS transport.
- The same test at 10 seconds: `bind` and the stream fail with `unavailable`,
  and the audit hook receives `clock_skew`.
- A `traceframe` test for the match: a deadline equal to the lease deadline and
  one 200 ms earlier are accepted; one later, and one more than a second
  earlier, are refused. Because the match is validation of untrusted input, it
  gets a property test beside the existing `Fuzz*` targets.
- The existing test that a node cannot extend beyond the admitted duration
  still passes unchanged.
- The trace module gate, `make check-trace-preflight`, and the main module
  tests for `internal/traceframe` and `internal/traceadmission`.

There is no stored state to migrate. Rolling back restores the exact rule, and
nodes that are behind fail again as they do today.
