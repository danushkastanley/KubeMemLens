# Local trace client verification

These are functional development checks on a two-node kind Kubernetes 1.37
cluster running LinuxKit arm64 with Cilium enforcement. They do not qualify
resource cost, other kernels, managed providers, independent review or adoption.

| Workflow | Observed result |
| --- | --- |
| Authenticated schema-2 preflight | Exact container lifetime checked; no worker or BPF attachment created |
| Bounded file trace | Completed on expiry with a validated, explicitly incomplete summary |
| Cancel before first metadata | Cancelled with confirmed API cleanup; missing terminal evidence remains unknown |
| Cancel during streaming | Cancelled with a validated terminal summary and confirmed API cleanup |
| One-event bound | Truncated with the event-limit termination reason |
| Pod replacement | Failed with `target_changed`; replacement Pod UID was not adopted |
| Explicit redacted export | Terminal states, nullable counters and provenance retained; target identities omitted |

A separate kernel ownership census verified removal of the worker and captured
BPF objects after each successful final case. Normal expiry and target replacement
had already removed the admission by the time the client sent its cleanup request.
The CLI correctly retained `cleanup: unconfirmed` and a nonzero exit for those
cases; the external census is separate evidence, not a client cleanup receipt.

The first run exposed cancellation before stream metadata being classified as a
failed session. The corrected coordinator accepts confirmed cancellation while
retaining the stream failure and unknown terminal evidence. Both early and
streaming cancellation were rerun. The first replacement fixture exceeded its
25-second deletion wait because it used the default 30-second termination grace;
the focused rerun used an explicit one-second grace for that temporary fixture.
The failed runs are retained in local evidence.

Every temporary installation was removed. The original prototype APIService was
restored and Available, added baseline seccomp files and test grants were removed,
and generated private keys were removed. No cloud resources were created.

Automated verification also covers:

- Real HTTPS CLI create/watch/cancel/export contracts and frozen Kubernetes
  endpoint/credential selection.
- A 100,000-event validated stream with bounded progress notifications and no
  retained raw event queue.
- The shared session consumed by the actual TUI update loop while navigating,
  resizing and receiving that maximum stream; terminal evidence and the selected
  lifetime remain intact.
- TUI state rendering at 40×10, 80×24, 120×30 and 180×50; explicit export consent,
  private atomic output and refusal to overwrite existing files.
- Permission denial, selected-target replacement, incomplete/trailing streams,
  uncertain creation, compensating cancellation and stateless preflight cleanup.

The default interactive policy omits raw paths. The maximum-stream fixture also
exercises the more demanding authorised event format and verifies that the client
and TUI still retain only metadata and a numeric summary.
