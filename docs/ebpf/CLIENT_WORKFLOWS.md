# Bounded trace workflows

The optional trace extension provides development-only file, cache and OOM
observations. Resource qualification remains incomplete. Installing the extension
or completing these workflows does not establish provider support or production
readiness. The ordinary memory agent and chart remain eBPF-free.

Trace access uses the verified Kubernetes HTTPS connection and the caller's
credentials. Ordinary memory read access does not grant trace access. The
extension must advertise `tracepreflights`, `traces` and `traces/stream` with the
expected verbs. Explicit HTTP collectors, service proxies and restricted evidence
mode cannot start these workflows.

## CLI

Select a namespace, Pod, container and trace type explicitly:

```sh
kubectl memlens --context local-test trace preflight example-pod \
  -n example --container worker --kind files --duration 15s --max-events 1000

kubectl memlens --context local-test trace run example-pod \
  -n example --container worker --kind files --duration 15s --max-events 1000
```

`preflight` returns a JSON report without creating an admission or loading a BPF
programme. Its baseline is the node service's startup observation, with its
capture time; it is not a fresh kernel probe or a resource qualification result.
`run` repeats preflight, then creates and watches one bounded session. The named
container's Pod UID, full container ID, start time and node are frozen before
preflight. Admission rechecks them. Refresh and replacement cannot retarget that
session.

Types are `files`, `cache` and `oom`. Durations must be whole seconds, from 1 to
300. The event ceiling is 100,000; server policy may impose lower bounds. Default
limits are 30 seconds, 10,000 events, 8 MiB output and 8 MiB map memory. Raw paths
are omitted. The client retains validated metadata and one numeric summary,
without retaining event payloads or process details.

For separate admission and activation:

```sh
kubectl memlens --context local-test trace create example-pod \
  -n example --container worker --kind cache --duration 15s --max-events 1000
kubectl memlens --context local-test trace watch ADMISSION_ID -n example
kubectl memlens --context local-test trace cancel ADMISSION_ID -n example
```

`create` prints the admission ID and pending expiry. Start `watch` before that
expiry. A recovered watch uses the server-held target and intent; it does not
select the Pod again. Watching activates an ephemeral stream, so it is not a
replay operation. There is only one reader. The client never automatically
retries creation or activation after an uncertain response.

Ctrl-C during `run` or `watch` requests cancellation through a separate control
connection and allows the bounded terminal stream to drain. Only a positive API
cleanup receipt is reported as confirmed. Absence, expiry, denial or a lost
response leaves cleanup unconfirmed. Completion, evidence coverage and cleanup
are reported separately. A missing terminal summary leaves engine counts and
observation windows unknown; event/output limits are shown as truncation.

## Explicit exports

```sh
kubectl memlens --context local-test trace run example-pod \
  -n example --container worker --kind files --duration 15s --max-events 1000 \
  --export trace-report.json --confirm-export

kubectl memlens --context local-test trace export ADMISSION_ID -n example \
  --export trace-report.json --confirm-export
```

`export ID` watches an existing admission and exports the result it receives. It
cannot retrieve an expired admission or a previous process's completed stream.
`watch` also accepts the export flags. `--export -` writes only report JSON to
stdout, with progress and the interactive summary on stderr.

Reports are bounded to 32 KiB. They include schema and stream versions, tool
version, engine/programme digests, bounds, timings, termination, nullable loss
counters, numeric correlations and caveats. Target names use report-local aliases.
Pod/container/node identities, session and binding identifiers, paths, raw events
and process details are omitted. Timings and counters remain sensitive operational
data, so every export requires explicit acknowledgement.

Files are staged privately with mode `0600` and published atomically. Existing
files and symlinks are refused unless the CLI's explicit `--overwrite` option is
provided. A partial or failed result retains its uncertainty in the export; it
never becomes a successful trace merely because a file was written.

## TUI

When the authenticated memory reader's Kubernetes endpoint advertises the trace
extension, the footer offers **T trace**. Select a container row and press **T**.
The trace connection is derived from that reader's frozen endpoint and identity;
changing the ambient kubeconfig does not redirect it.

- **f/c/o** chooses files, cache or OOM.
- **d** cycles duration; **l** cycles event limits.
- **Enter** runs preflight. Review the frozen target and limits, then press
  **Enter** again to start.
- **s** requests cancellation. **Esc** returns to navigation while tracing
  continues; **T** reopens the same session.
- After termination, **x** selects an export file and **y** confirms the
  sensitivity notice. TUI exports never replace an existing file.
- **n** selects the current container for a new session only after the previous
  session has ended. Quitting requests cleanup before releasing the transport.

Refresh, navigation and resize use the existing dashboard behaviour. Trace
updates are coalesced wake-up signals, not an event table or queue. Revoked memory
access clears displayed trace evidence and requests cancellation.
