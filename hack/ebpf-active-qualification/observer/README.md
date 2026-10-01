# Standard-service observation for local qualification

This administrative Linux helper measures existing agent and collector telemetry
without loading BPF, changing listeners, enabling legacy reads or starting a
workload. It is outside the product binaries. It emits an initial numeric record
and one record per second for a fixed 1–1,800 second window.

Build from the repository root:

```sh
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -buildvcs=false \
  -o standard-observer ./hack/ebpf-active-qualification/observer
```

Use `GOARCH=amd64` for an amd64 observer. Execute only on the explicitly selected
owned Linux Node, with its ordinary `/proc` view and `/usr/bin/nsenter` available.
The administrative observer needs permission to inspect both approved processes
and enter the agent's network namespace. This does not add capabilities to the
standard agent, collector or optional tracer.

## Private configuration and ownership

`standard-observer --config PATH` requires a regular, non-symlink file of at most
32 KiB, with no group/other permissions. All fields below are required. Duplicate,
case-aliased, unknown or missing configuration keys fail. Configuration values
must be resolved by the external controller from the actual owned installation:

| Fields | Required source |
| --- | --- |
| `seconds` | Frozen observation duration, 1–1,800 |
| `bootID` | Independently verified selected Node kernel boot ID |
| `agentPID`, `collectorPID` | Fresh full-CRI-ID process resolution on the same Node |
| `agentStart`, `collectorStart` | Verified `/proc/PID/stat` start ticks |
| `agentContainer`, `collectorContainer` | Full 64-character CRI container IDs |
| `agentSHA256`, `collectorSHA256` | Approved executable SHA-256 digests |
| `server` | Explicit local Kubernetes HTTPS endpoint |
| `token` | Short-lived token granted only `get` on the metrics resource |
| `caPEM` | The selected Kubernetes API's private trust roots |

The controller must also bind the Node and Pod UIDs, controller ownership,
immutable images, rendered configuration, service routing and unchanged process
lifetimes. Both services must be on the selected Node; this helper does not claim
cross-Node placement support. Verify those bindings before and after the window.
A shared kernel boot ID in kind does not replace Kubernetes Node identity.

PID file descriptors remain open throughout the window. Each poll rechecks process
lifetime, full container membership and executable digest. Process loss or changed
identity ends the observation. The namespace and helper executable are passed to
`nsenter` as pinned descriptors. Its child can only read the fixed agent URL
`http://127.0.0.1:8082/metrics`; it receives no configuration or bearer token.
The `--agent-scrape` mode is the internal numeric child operation.

Collector reads use the authenticated route
`/apis/memory.kubememlens.io/v1alpha1/metrics/current`. Its health-only HTTP port is
not a metrics fallback. TLS uses only the supplied CA, requires TLS 1.3, and permits
no proxy or redirects. Endpoints are limited to explicit HTTPS loopback addresses,
local kind control-plane names, or `kubernetes.default.svc:443`. This endpoint
allow-list grants no cloud execution authority.

## Bounds and evidence

Transport, headers, bodies, subprocess time and output are bounded. Raw metric
text and Kubernetes metadata are parsed in memory and discarded. Only the closed
numeric projections enter stdout; failures emit a fixed diagnostic category.
Durations round upward to integer nanoseconds. Unknown labels within selected
metric families, duplicate series, malformed numbers and incomplete responses
fail. Unrelated families, including identity-labelled metrics, are omitted.

A rejected collector response ends the observation immediately. Its diagnostic
includes only the fixed failure category and numeric HTTP status (100–599), never
the response body, headers, URL or token. Transport errors remain category-only.
No response is retried or counted as a successful sample.

Each record includes wall/monotonic clock correlation, uncertainty, actual read
span, observer CPU including reaped reader children, and a conservative sum of
parent/child peak RSS. This peak RSS is observer accounting, not a working-set
measurement. A blocked output cannot keep the process alive past the duration
plus ten seconds. Interrupted, missing or delayed records stay incomplete; the
helper does not grant a performance pass or retime a missed deadline.

Use `standard_window.py` to validate the complete stream before analysis. It counts
new agent scan attempts once, checks failure/reset counters and retains collector
requests whose individual durations were missed between polls. `scans.py` brackets
whole scans using the production completion timestamp's one-second precision and
the separate owned-attachment witness. Its p95 comparison is strictly below 5%.

A bounded local integration verified 21 records, four new successful scans and
four accepted ingestions through the real namespace/TLS/authorisation paths. It
also verified unchanged service lifetimes and removed the owned installation,
reader grant and private Node configuration. That integration is not a steady-state
performance result, managed-provider qualification or an active-trace scan test.
