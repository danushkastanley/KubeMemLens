# Bounded local lifecycle qualification

Date: 16 September 2026. This report covers BPF-007 on one local Linux profile.
It does not establish performance, provider support, independent review or R7 readiness.

## Candidate and scope

The incident worker and six signed programme manifests are unchanged from the
accepted BPF-006 candidate. The optional API adds an installation-only choice of
one or two node traces, within the existing hard ceiling; the default remains one.
Unsupported collection operations now return HTTP 405 so the Kubernetes namespace
controller can complete ordinary deletion. No collection access or permission was added.

| Identity | SHA-256 |
| --- | --- |
| Engine release | 49f6beac38ff9310a3863b7bad3f14854890fba4403d7d49d170b310eb854e08 |
| Programme index | a968adef5da2266edffafe054ab8b3ed782de181a363974636eebda30d9983c2 |
| Local image | 4a957b8fa80324e9ce8a66d6876766eb4d1ea13ea85a83973b245e4238b741a3 |
| Node/API executable | e4f071c6eb61aedde3e0ebe12f4713b5bc524b6210e555c83d8fb57433becab3 |
| Accepted arm64 worker | 6fbd2d17ea799fe84f57113454bf2071ea5cea1c741fa5529eb9fcbc466c1ffd |
| Administrative census helper | ec0d03526a976f97f04ee47f73ff7201979d1c4bc1010bfe327e21dc52999100 |

Execution used Kubernetes 1.37.0, LinuxKit 7.0.12 arm64 and Cilium 1.20.1 on the
owned two-node kind cluster. The node service retained BPF/PERFMON and its accepted
seccomp policy; the API remained capability-free. Both services retained two-CPU
and 512 MiB limits. Fixture Pods had 500m CPU, 64 MiB memory, no token, dropped
capabilities, read-only roots, restricted admission and a 30-minute active deadline.
No new OOM consumption or global-OOM experiment was performed in this ticket.

## Runtime coverage

All cases used authenticated Kubernetes aggregation and the accepted worker.
The current image passed 81 distinct ordinary lifecycle/limit cases, six partial
attachment cases, four paused-reader flood cases and six lower-map-budget cases.
Exploratory failures, inconclusive attempts and repeated cases were retained separately.

| Cases | Scope and outcome |
| --- | --- |
| Expiry, cancellation, disconnect, worker TERM/KILL | Files, cache and OOM, each at one and two traces; 30 passing cases |
| Node TERM/KILL and API KILL | All three kinds at one and two traces; 18 passing cases; owned cleanup below the ten-second gate |
| Revocation, Pod deletion/recreation, container restart | All three kinds at one and two traces; 24 passing cases; replacement lifetimes were not followed |
| Node concurrency | Third distinct-principal admission rejected with 429 while two traces were active, for each kind |
| Lower event, encoded-byte and path ceilings | File traces at one and two concurrent sessions; six passing cases |
| Partial attachment | All three kinds at one and two traces; an inactive, incomplete link set was witnessed before termination; final-helper teardown bounds were 0.300–1.430 seconds |
| Paused-reader flood | Short and 230-byte paths at one and two traces; readers remained paused until owned cleanup; event limits or the admitted deadline ended the trace |
| Lower map budget | A 4096-byte admitted budget failed before load for each kind at one and two sessions; explicit engine failure, unknown engine counts and zero delivered events |

Ordinary teardown measurements were below two seconds; the largest observed
ordinary bound was 1.747 seconds. Short-path floods each stopped at 10,000 events,
with under 8 MiB encoded output and teardown below 1.25 seconds from flood start.
Long-path floods expired and cleaned up within 0.724 seconds of the earliest
admitted deadline. No downstream write-timeout is inferred from those expiry results.

Resource samples confirmed the configured CPU/memory ceilings, the observed 19,206-PID cgroup ceiling,
no service OOM kills and bounded owned map allocations. File maps consumed 458,000
kernel bytes plus a 540,672-byte mapping reservation per worker on this profile,
below the admitted 8 MiB map budget. The fixed ring remained 256 KiB. These are
ceiling checks, not the BPF-008 CPU, working-set, latency, regression or loss benchmarks.

Six attempts to raise server ceilings or supply a client concurrency override
were rejected with 400. Separate explicit capless Linux tests attempted the existing
fixed non-attaching preflight load at one and two concurrent calls; policy denied
both. This establishes a denied-load path, not rejection of altered incident bytecode.

## Ownership, recovery and output

The [administrative helper](../../prototype/trace/qualification/lifecycle/README.md)
binds the node process to its image hash, full CRI container ID, process lifetime
and pidfd. Workers must match the accepted executable, parent and selected cgroup
descriptor. Captured map/programme/link IDs come only from those workers' descriptors;
checks open only captured IDs and close every inspection reference. Missing evidence
and excluded children remain unknown. No global BPF enumeration or unpin operation runs.

For partial attachment, the helper retains a hash-verified immutable executable,
then uses administrative syscall stops to witness a successful first link while
control remains inactive. It modifies no programme, syscall argument, register or
worker security profile. Exit-kill and the unchanged node supervisor bound observer
failure. This is external qualification instrumentation, not a worker capability.

Every assessed run ended with no selected workers and no remaining captured owned
objects. Persistent pins remain prohibited by the worker policy and object contract.
A pre-existing Cilium map retained its identity and shape across the campaign.
Three additional two-trace partial-attachment repetitions preserved a known Cilium
network link and its programme, including link/programme IDs and programme tag;
all three controls were excluded from the owned object sets. Unknown process
selection and cleanup uncertainty also have native negative tests.

Node-process replacement deliberately cannot acknowledge predecessor cleanup.
The API retains that reservation and quota, producing 429 even after the replacement
is healthy. Administrative recovery first verified captured IDs absent and the new
node idle, then restarted only the owned API Pod with a UID precondition. Kubelet
restart backoff and service recovery time are recorded separately from kernel teardown.
No slot is freed merely because a process identity or timeout changed.

Writable authorised streams produced one terminal summary. Permission loss removed
rich aggregates and context. Client disconnect or API death produced an explicitly
incomplete transport, with no invented terminal frame. Event rows, paths and victim
process values were not retained in qualification records; only bounded counts,
resource samples, identities and termination/cleanup evidence were saved.

## Verification boundary

Root, optional-module and worker native Linux race suites, vet and Linux arm64/amd64
builds passed. The final census helper also passed targeted native race/vet checks,
seven tests in a capless Linux container and both cross-builds; its arm64 binary reproduced
between the host cross-build and pinned native compiler. Renderer, repository contract,
format and diff checks passed. Standard agent sources, dependency files, charts and
release configuration are unchanged, and its dependency closure contains no BPF loader.

Node UID replacement, malformed verifier outcomes, cleanup failures and retained
callbacks have contract/fault-injection coverage. The shared kind Node objects were
not deleted to manufacture a UID change, and no unreviewed incident object was loaded
to force a verifier rejection. Full attachment, target changes, process loss and
partial-attachment teardown have the separate real-kernel evidence above.

The host's Xcode licence remained unaccepted. Native checks ran in the pinned Linux
compiler; inspected Makefile contract recipes ran directly. A compiler temporary-space
failure was retained and the affected worker checks passed with disk-backed temporary
storage. This did not change incident limits or test assertions.

Linux/arm64 vulnerability scans found no called-symbol findings in the root or launcher.
Module/package findings remain visible; the worker retains its documented containerd
findings and unmaintained OpenPGP dependency finding. None is waived; see the
[dependency record](FILE_CACHE_DEPENDENCIES.md). Independent review and the complete
BPF-008 benchmark/decision gates remain open. EKS remains deferred until all phases.

Rollback stops new admission, confirms owned teardown and removes only the optional
resources. It does not require a standard-agent, chart or stored-data migration.
