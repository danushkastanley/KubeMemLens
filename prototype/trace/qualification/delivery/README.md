# Bounded event-delivery observation

The separate `ObserveCeiling` parser observes admitted event/output ceilings. It
uses the same production frame validation and exact admission/target bindings,
retains nullable engine counts, and emits only numeric evidence and a terminal
reason. An event ceiling requires the admitted event count. An output ceiling
requires a validated output-limit summary with less than one maximum-sized frame
remaining after the terminal reserve; it does not require every byte to be used.
Ordinary expiry, cancellation and a premature ceiling label remain inconclusive.
Missing or extra frames remain incomplete transport. The parser does not infer
ring saturation, resource safety or latency/loss percentiles from unknown counts.

`ConnectCeiling` uses the shared authenticated transport and active-admission
lookup, with the same allowed origins, private CA, TLS 1.3 minimum, no redirects,
no proxy, bounded headers and 40-second request lifetime as `Connect`. It starts
only the supplied existing admission and always closes its stream. The normal
`Observe` contract below is unchanged.

The Linux CLI selects this observer only with `--observation ceiling`; omitting
the option retains normal latency observation. Both modes share the private-file,
bound specification, kernel boot-identity and 45-second process lifetime guards.
Ceiling mode first emits one `case: "ceiling-ready"` record after the received
metadata matches the authenticated active admission. The controller must wait for
this before starting its burst, preventing an early limit from racing that lookup.
The final record identifies `case: "ceiling-observation"` and contains no latency
percentiles. Complete transport without a reported ceiling returns a failure
status while retaining its numeric result. A failed readiness write aborts the
observation. Normal mode retains its existing single-document output.

`--observation paused-reader` uses the same checks but emits
`paused-reader-ready`, pauses application reads for five seconds, then resumes.
Its final `paused-reader-observation` retains requested/actual duration and
completion, including failed observations. HTTP and kernel buffering can continue
during this pause: it alone proves neither producer backpressure nor ring loss.
The controller still needs independent saturation, resource and cleanup evidence.
Normal mode remains the default; all transport and process limits remain.

This qualification reader attaches to an **existing approved file admission**
through Kubernetes aggregation. Attaching starts the incident worker. It is not a
passive metrics read and must run only after the controller's bounded local test
scope, fixture ownership and candidate profile have been verified.

The portable `delivery` package uses the production `traceframe.Reader` and
`MatchAdmissionVersion`. It checks frame order, cumulative byte/event limits,
version, target binding, engine/programme digests, disclosure policy and bounds.
After receiving metadata it reads the authenticated **active** admission and
matches its deadline. The POST response's pending 15-second expiry is not reused
as the running trace deadline.

Only confirmed-path file streams are accepted, and every path must decode to the
owned fixed fixture path `/work/fixed-seed.bin`. Paths and raw frames are inspected
only in memory and never returned. Results contain timestamps, counters and fixed
state fields; target identities, session IDs, credentials and paths are excluded.
A complete transport preserves `hookCoverageIncomplete=true`: file-hook blind
spots are not reclassified as complete filesystem coverage.

Terminal diagnostics retain the validated termination and correlation state,
alignment uncertainty, end-to-receipt difference and counters even when those
fields prevent qualification. They contain no raw frame or identity. Missing or
invalid correlation still fails the same transport and latency criteria.

A rejected wall/monotonic clock comparison retains its elapsed readings and one
immediate follow-up reading as `clockDiagnostic`. These four numeric values can
show whether the mismatch persisted; they do not establish its cause. The
observer still fails on the original reading, does not retry the frame and never
uses the follow-up to qualify transport or latency. Successful observations take
no additional clock readings, and the 5 ms rejection bound is unchanged.

## Clock and latency meaning

Run `../delivery-client` on the worker's Linux kernel. Its private configuration
must contain the worker's independently resolved kernel boot ID; the command
checks the local boot ID before and after attachment. The controller must also
verify the intended Node placement and immutable target. Shared-kernel kind Nodes
have the same clock; the boot check does not claim a distinct Kubernetes Node UID.

The worker projects kernel monotonic timestamps through its measured alignment.
The reader timestamps after production frame validation, conservatively including
client decoding delay. It requires a known overlapping terminal correlation and
at most 5 ms SDK alignment/drift uncertainty. It also measures local wall-versus-
monotonic drift using `time.Now` values and rejects drift above 5 ms. Both bounds
are added to the observed delivery delay. A clock contradiction is unavailable
measurement, not negative latency or zero delay.

The first-event delivery bound, delay after the reader starts, and nearest-rank
p50/p95/p99 are separate fields. The latter delay starts after HTTP headers; it is
not admission/preflight/attachment latency. Missing events sort after delivered
events, so loss cannot improve a reported percentile. An unbounded percentile is
`null`. The maximum field covers delivered events only. The normal gate requires
p95 <250 ms, p99 <1 s and loss strictly below 0.1%, with no undeclared sampling or
malformed-record rejection. Exact threshold equality fails. Counts must reconcile;
unknown counters never become zero. Complete over-budget evidence remains a
failure, not incomplete transport to discard and retry.

## Transport and execution bounds

The HTTPS transport uses only the supplied CA, TLS 1.3 or newer, no environment
proxy, no redirects, 5-second connection/header/handshake deadlines and a
40-second total context. Two connections allow the active-status read while the
stream is open. Endpoints are limited to explicit loopback addresses, local kind
control-plane hostnames, or `kubernetes.default.svc:443`. This endpoint allowance
does not authorise creating a cloud cluster or claim provider compatibility.

The Linux command accepts `--config PRIVATE_FILE`. It rejects symlinks,
non-regular files, group/world-readable files, unknown/trailing input and files
above 32 KiB. The command's 45-second hard exit also bounds blocked output.
The private JSON fields are:

- `server`, `token`, `caPEM`, `workerBootID`;
- `sessionID`, `engineDigest`, `programmeDigest`;
- `target`, using the explicit `trace.TargetIdentity` field names;
- `durationSeconds` (1–30), `maxEvents` (at most 10,000), `maxOutputBytes`
  (at most 8 MiB), `maxMapBytes` (at most 8 MiB), `maxPathBytes` (at most 256).

Write the config from fresh owned fixture/admission evidence with mode 0600.
Remove only that verified file after the helper exits. The controller must cancel
the admission on every failure and independently verify worker/BPF teardown; the
reader does not substitute for lifecycle census. A failing run retains its bounded
numeric result when available and returns nonzero. Never log the configuration or
include it in a public evidence bundle.

## Qualification status

Host race and native Linux tests exercise real TLS transport, an untrusted CA, active-status
matching, cancellation/redirect rejection, the production frame codec, privacy,
missing data, clock uncertainty and exact latency/loss boundaries. Both Linux
architectures build. The combined ceiling/paused-reader delivery and client suite
passed 29 native Linux tests on 1 October 2026, with no skipped tests or retained
containers. The real admitted flood stream and full paired campaign still require
execution on the owned environment. HTTP fixtures in tests are synthetic protocol
tests, not kernel evidence.

The currently installed aggregate-only file profile does not permit confirmed
paths. Any profile change and its qualification scope must be recorded separately;
this helper does not enable that policy or qualify the existing candidate by itself.
Default aggregate summaries cannot establish event-to-client percentiles.
