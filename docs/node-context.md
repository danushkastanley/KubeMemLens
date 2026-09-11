# Optional Node-context contract

Status: bounded one-shot collection implemented; authenticated publishing and
the chart profile remain pending. The current chart rejects
`nodeContext.enabled=true`. Managed-provider support remains unqualified. See
[ADR 0007](adr/0007-isolate-node-context-producers-and-reads.md).

## Transport and preflight

Use a dedicated non-root Linux DaemonSet, binary and ServiceAccount. Drop all
capabilities, use RuntimeDefault seccomp, a read-only root filesystem and explicit
projected tokens with automount disabled. No hostPath, hostNetwork, hostPID or
privileged mode. The standard agent and its permissions remain unchanged.

Read the authenticated identity through a bounded `SelfSubjectReview` before
the Node GET. Require a ServiceAccount, Pod UID, credential ID and Node name/UID
extras. The configured Node name must match the authenticated claim, and the
Node GET must return the same UID. This prevents a flag override or Node-name
reuse from redirecting the producer. Then select an InternalIP
and reported kubelet port. Fix HTTPS and `/stats/summary`; do not accept arbitrary
URLs, query parameters or environment proxies. Reject redirects. Verify the
serving chain and target IP SAN against an explicit trust bundle. The API server
CA is not assumed to sign kubelet serving certificates. A DNS endpoint needs a
separately qualified SAN/address-binding design, not an automatic fallback.

Audience configuration is explicit and qualified for each destination. Ingestion
uses the API server audience; kubelet TokenReview acceptance must be tested.
One projected token may serve both only where that audience is accepted by both.
Reload token files through rotation. The collector receives authenticated claims
from aggregation, never a kubelet bearer token. Do not expose tokens, headers or
raw transport errors in observations, logs or metrics.

Before recurring collection, preflight must establish Linux/cgroup-v2 eligibility,
supported kubelet version, scheduled identity, trusted TLS, authentication and
stats permission. A bounded Summary request proves access; a self access review
alone cannot prove kubelet reachability or audience acceptance. Positive default
qualification covers Kubernetes 1.36 and 1.37. Other minors remain explicitly
unsupported for this profile until separately qualified. This is a product
qualification boundary, not a claim that stats RBAC first appeared in 1.36.

Unsupported profiles, untrusted TLS, missing SANs and invalid audiences stop
preflight. Never disable TLS, use the insecure port, auto-approve CSRs, modify
provider firewall policy or fall back to a proxy. Do not probe healthz, configz,
pods, metrics, logs or exec. Typed reasons distinguish invalid target, untrusted
TLS, authentication, denial, timeout, unreachable, throttling, malformed,
oversize and unavailable source. Missing optional fields mean partial evidence.
Successful transport does not establish memory health.

## Permissions and residual risk

The [RBAC contract](security/node-context-rbac.json) contains unbound role
definitions for verification. It is not an installation manifest; the chart does
not render these roles yet.

| Principal | Permission | Deliberately absent |
| --- | --- | --- |
| Optional producer | Core `get nodes`, `get nodes/stats`; aggregated `get ingestionepochs`, `create nodesnapshots` | Node list/watch, metrics, proxy, logs, exec, Pod reads, Secrets and token creation |
| Node-context viewer | Aggregated `get/list nodecontexts`, `get nodecontexts/history` | Core kubelet access, ingestion, Pod identities and workload metrics |
| Standard agent | Existing cgroup producer permissions | All new kubelet and Node-context read permissions |
| Namespace viewer | Existing scoped Pod/workload/history permissions | Full Node measurements, system totals and cross-tenant gaps |

Kubernetes maps `/stats/*` to `nodes/stats`. No `nodes/metrics` is needed here.
`nodes/proxy` can permit container execution and is forbidden. See
[kubelet authorisation](https://kubernetes.io/docs/reference/access-authn-authz/kubelet-authn-authz/).

A shared ClusterRole cannot dynamically restrict each Pod to its scheduled Node.
Static resource names do not solve rescheduling without another binding mechanism.
A stolen token can read another Node's stats within its RBAC scope. Application
own-node validation is not token containment. Disclose that risk at installation.
CNI egress restrictions are defence in depth; portable NetworkPolicy does not
prove own-node isolation. Record actual enforcement for each qualified profile.

Identity preflight also requires `create selfsubjectreviews` in
`authentication.k8s.io`, normally granted to authenticated principals by
`system:basic-user`. It returns the caller's authenticated attributes and does
not persist an object. If this default permission is absent, preflight fails;
the producer does not grant itself access or trust unsigned local JWT contents.
See [Kubernetes identity review](https://kubernetes.io/docs/reference/access-authn-authz/authentication/#api-access-to-authentication-information-for-a-client).

## Field inventory and privacy

`internal/nodecontext/types.go` defines the normalised observation. Optional
numeric pointers distinguish unavailable values from measured zero. There are
no per-Pod Summary, volume, filesystem, network, process, path or label fields.

| Fields | Classification | Retention and export |
| --- | --- | --- |
| Node name/UID | Infrastructure identity | Internal latest/history binding; name only in authorised reads/capture; UID excluded from default capture and all operational telemetry |
| Start/sample/report/receive times | Operational evidence | Latest, bounded history and capture; preserve independent source clocks and counter baselines |
| Node usage, available, working set, RSS | Cross-tenant aggregate | Node-context reads/capture only; no new workload metric series by default |
| Memory faults, PSI and swap | Cross-tenant aggregate | Same policy, with source units and separate memory/swap timestamps |
| System-container memory/swap | Cross-tenant aggregate | Only kubelet, runtime, misc and pods categories; no arbitrary names |
| Capacity, allocatable, MemoryPressure | Kubernetes status | Separate source/time; neither allocatable nor capacity is current free memory |
| Hugepage capacity/allocatable | Kubernetes status | At most 16 resource entries; do not conflate capacity, allocatable and workload requests |
| Availability, reason, caveats | Fixed operational codes | At most 16 ASCII caveat codes, 64 bytes each; no upstream free text or names |
| Pod/workload contributors | Tenant workload identity | Derive only from independently authorised cgroup data; not retained from Summary |
| Raw Summary | Cross-tenant sensitive transport | Bounded transient decoding only; discard uncollected sections; never log, persist or capture bodies |
| Tokens/kubeconfig/private keys | Credential | Excluded from the data model and retained evidence; public CA bundle is mounted configuration |

Memory usage, working set, RSS and available overlap. Summary has no physical
total or explicit per-measurement provenance marker. Use separately labelled
Node capacity and unknown provenance unless a profile supplies evidence. Memory
and swap retain their own timestamps. PSI cumulative totals are nanoseconds;
cgroup text totals use microseconds. See the
[versioned Summary types](https://github.com/kubernetes/kubernetes/blob/v1.37.0/staging/src/k8s.io/kubelet/pkg/apis/stats/v1alpha1/types.go)
and [source documentation](https://kubernetes.io/docs/reference/instrumentation/node-metrics/).

Reject invalid collected types, duplicate selected fields/categories, invalid
units/ranges, integer overflow and trailing JSON. Skip unrelated upstream fields
within depth/token/byte bounds so legitimate CPU/storage data does not break
parsing. Unknown system categories are discarded with a fixed partial-evidence
code. Downstream ingestion remains strict. Never retain raw Pod/volume sections.

## Source, response and storage bounds

`internal/nodecontext/limits.go` owns the ceilings. Configuration may lower
capacity; raising a ceiling needs a contract review. These are not measurements.

| Limit | Contract |
| --- | --- |
| Collection | 15-second interval, bounded 10 percent jitter, one request in flight, five-second whole-request deadline |
| Backoff | Transient failures back off exponentially to 60 seconds, reset on success; no overlapping retry or catch-up burst |
| Summary response | 4 MiB including skipped sections; nesting at most 32 and at most 250,000 JSON tokens |
| API preflight | 64 KiB identity review; 1 MiB Node response; at most 64 addresses and 64 conditions; all requests share the five-second deadline |
| Normalised observation | 16 KiB; Node name 253 bytes and UID 128 bytes with validated ASCII identity syntax |
| Nested fields | Four known unique system categories, 16 hugepage resources of at most 63 bytes, 16 fixed caveat codes of at most 64 bytes |
| Freshness | Stale after 45 seconds; future skew at most 30 seconds; report age at most two minutes, also subject to configured lower limits |
| Accounting | At most five-second skew across required samples; stale or missing inputs suppress gap calculation |
| History | 15 minutes, 61 points per Node instance, 1,000 instances and 64 MiB globally encoded |
| Reads | At most 100 Node-context records per page, existing 16 MiB response ceiling, one selected Node history with bounded instance pagination |
| Admission | Existing Node ceiling; at most two active producer roles per Node; bounded retired identities, per-stream rate and overall Node/concurrency limits |

The max-width encoding test fills every optional value, all categories/maps and
maximum-length fields. The result must fit 16 KiB. Check envelope bytes against
the configured ingestion ceiling too, even though the Go default is 4 MiB and
the chart default is 8 MiB.

At the record ceiling, 5,000 latest records occupy 81,920,000 encoded bytes. With
67,108,864 history bytes, that is 149,028,864 bytes or 142.125 MiB before heap,
indexes and decoding. A conservative planning factor of four is 568.5 MiB for
new data alone; it is not a measured heap bound. Do not claim this capacity under
the current 256 MiB collector limit. Integration must benchmark actual store heap
and specify optional-profile capacity/resources before enablement. Standard
resources remain unchanged. Reject capacity overflow atomically and report
history eviction or truncation as coverage loss.

## Ingestion invariants

The collector requires the configured producer ServiceAccount and authenticated
Pod UID, Node name, Node UID and credential ID. Match current inventory Node UID
before Node-context joins; a reused name represents a new Node instance.

1. Derive role from authenticated identity. Configured producer usernames must
   differ. A disabled role cannot post, and JSON cannot select another role.
2. Key ownership by Node UID and role; replay state by Pod instance and role.
   Replacing either producer retires only that stream. Old Node instances cannot
   revive through another stream.
3. Apply epoch, increasing sequence, deterministic duplicate/conflict and bounded
   retired-instance rules independently to both streams.
4. Reject Node context from standard producers and container data from optional
   producers. Node reports never call container snapshot replacement.
5. Validate outer report and nested sample times. Failure or duplicate reports
   do not refresh old evidence or append duplicate history points.
6. Keep last good evidence separately from current source failure. Collector
   restart means rebuilding/missing history, never a complete empty interval.
7. Negotiate schema 3 while retaining schemas 1/2. Older collectors decline the
   optional producer; legacy readers never receive new fields.
8. Enforce byte, rate, identity, Node, history and response limits before expensive
   processing. Source isolation does not remove overall admission limits.

## Read policy and accounting

Every request uses current exact delegated authorisation. Node-context permission
alone gives no Pod identities/counts or Pod-based gap. Full analysis requires
cluster-wide Pod list authorisation before contributor lookups. Namespace-only
views never subtract visible Pod charge from global usage. Debug, errors,
pagination, history and caches obey the same boundary. Revocation clears protected
client state and blocks new live exports. Transport failure can retain the last
authorised frame with visible age; it cannot widen scope.

Pure analysis accepts source-labelled, authorised inputs. A Node usage minus
observed Pod charge estimate requires compatible definitions, aligned times and
complete coverage. It includes system memory. Subtract system usage only after
qualification proves disjoint categories; never subtract both the `pods` system
category and observed Pod charge. Floor negative gaps at zero, preserve the
discrepancy and reduce confidence. Hugepage reservations remain separate and
must not be subtracted twice from source-adjusted availability.

Counter rates require matching Node/start identity and increasing values. Swap
allocation alone is not active pressure. Node severity cites measured PSI,
recent OOM, swap activity or MemoryPressure using Node-specific rules. No workload
mutation, eviction or rescheduling is part of this feature.

## Capture, verification and rollback

The separate `memlens-node-context` command requires explicit `--once` selection,
an in-cluster Pod-bound credential, a kubelet CA bundle and a token file with a
verified audience. It checks its own `/proc/self/cgroup` for memory-controller
mode; no host mount is needed. The source gets only its scheduled Node object
and direct `/stats/summary`, after the authenticated self-identity review.
Redirects and environment proxies are disabled.
Token and CA files reload for every admitted attempt. Repeated reads are spaced
at least 13.5 seconds apart, allowing the planned 15-second interval's jitter.

```sh
memlens-node-context --once --node-name "$NODE_NAME" \
  --kubelet-ca /trust/ca.crt \
  --kubelet-token-file /var/run/secrets/kubernetes.io/serviceaccount/token \
  --output /output/node-observation.json \
  --metrics-output /output/node-operational.prom
```

The output is a schema-1 `NodeContextDiagnostic` containing a normalised
observation with its internal Node UID redacted. It is separate from the incident
formats. New output files are mode 0600 and existing
files are never overwritten. Omitting `--output` writes the observation to stdout.
Operational metrics contain fixed reason labels, request duration and response
bytes, without Node names or credentials. No metrics listener is installed.

Run the disposable local verifier with:

```sh
NODE_CONTEXT_ACKNOWLEDGE=create-and-remove-node-context-kind \
NODE_CONTEXT_ARTIFACT_DIR=/path/to/new-evidence \
hack/verify-node-context-kind.sh
```

It creates its own kind cluster and a local scratch image containing the real
producer, signs an IP-SAN serving certificate inside that disposable Node, and
verifies Pod-bound token access, stats permission denial and CA rejection. It
never changes an existing cluster, weakens TLS, or grants producer proxy access.
The certificate bootstrap is test setup, not provider certificate automation.
The sanitised result records the exact Node image, kernel, runtime, architecture,
binary digest, field availability and cleanup. NetworkPolicy and cloud profiles
remain unqualified. The CI workflow repeats it for Kubernetes 1.36 and 1.37.

Node-context capture uses incident schema 4, private files and the existing
64 MiB read/write cap. Deep schemas 1/2 and restricted schema 3 keep their meaning.
Reject Node-context export to older schemas instead of silently dropping data.
Default captures exclude internal UIDs, container IDs, paths, credentials and
unqualified system names. Contributor display names follow the caller's access.

Run `go test ./internal/nodecontext` and `hack/test-node-context-contract.sh` for
field representation, size, negative permissions and disabled chart policy.
Contract checks alone do not prove runtime authentication or collection. Adapter
tests add hostile TLS/JSON, token and CA reload, cancellation, rate and fuzz
checks. The local verifier exercises real direct-kubelet authentication.
Ingestion integration must still add two-producer isolation, token rotation,
revocation, Node replacement, restart, capacity and local lifecycle evidence.
Each provider needs exact certificate, audience, CNI, kernel/runtime, version and
artefact evidence before a support claim.

Disable/remove optional producers and bindings before collector downgrade.
Standard cgroup collection and observed-charge Node views remain usable. Storage
is ephemeral; retain a schema-4-capable reader for new captures.
