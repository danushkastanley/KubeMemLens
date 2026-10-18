# Trace audit, quotas and retention

The optional trace controller emits versioned JSON audit records containing fixed
categories and keyed references. It does not log trace events, paths, process
arguments, raw actor names, workload names or runtime identifiers. This remains a
development profile; audit functionality does not establish resource or provider
qualification.

## References and installation key

Provision a separate administrator-owned, immutable Secret containing exactly 32
random bytes under `reference.key`. The optional chart requires its name in
`auditReferenceKeySecret` and the lowercase SHA-256 of those bytes in
`auditReferenceKeySHA256`. The non-loading installation hook and API startup both
verify the digest. Only that hook and the API mount this key; node workloads do
not receive it. The chart does not create the Secret or place its contents in
Helm values or release state.

With the context, namespace, new Secret name and protected local key path selected:

```sh
set -euo pipefail
umask 077
openssl rand -out "$AUDIT_KEY_FILE" 32
kubectl --context "$TRACE_CONTEXT" --namespace "$TRACE_NAMESPACE" \
  create secret generic "$AUDIT_KEY_SECRET" \
  --from-file=reference.key="$AUDIT_KEY_FILE" --dry-run=client -o json |
  python3 -c 'import json,sys; d=json.load(sys.stdin); d["immutable"]=True; json.dump(d,sys.stdout)' |
  kubectl --context "$TRACE_CONTEXT" create -f -
python3 - "$AUDIT_KEY_FILE" <<'PY'
import hashlib, pathlib, sys
print(hashlib.sha256(pathlib.Path(sys.argv[1]).read_bytes()).hexdigest())
PY
```

Use a dedicated key per installation. Store it separately from ordinary logs and
exports. Rotation uses a new immutable Secret and digest followed by a reviewed
upgrade; running processes never silently reload a different key. Retain old
keys only under the administrator's historical-verification policy.

Records identify the key by its SHA-256, and references use HMAC-SHA256. The
versioned input is the UTF-8 prefix `kubememlens-trace-audit-v1/DOMAIN`, followed
by each field's four-byte unsigned big-endian byte length and UTF-8 bytes:

| Domain | Ordered fields |
| --- | --- |
| `actor` | Authenticated name, UID; optional UID may be empty |
| `tenant` | Kubernetes namespace |
| `session` | Server-generated admission ID |
| `target` | Namespace, Pod name, Pod UID, container name, full container ID, UTC RFC3339Nano start time, Node UID, decimal cgroup ID |

Target references are created only after validating the node-bound lifetime.
Mutable groups and extra authentication claims do not change actor references or
create extra quota buckets. Administrators can verify a reference using the
protected key and candidate identities from their authorised inventory. References
are not reversible lookup records, anonymous data or digital signatures. Protect
log access and integrity through the cluster's logging controls.

## Record meaning and delivery

Each record is at most 2 KiB and contains schema version, UTC time, key ID, policy
fingerprint, actor class, available references, operation, decision, fixed reason,
trace kind and frozen limits. A rejected request does not disclose another
session's target. A rejected reservation does not invent an engine termination.

The controller records configuration, preflight, admission, reads, activation,
cancellation, terminal outcomes and cleanup. Terminal outcomes distinguish
`validated_stream` from `controller` decisions. Kernel cleanup is a separate
record: a terminal decision alone does not prove that owned BPF state is gone.
Repeated attempts to report the same terminal outcome join one record.

Audit delivery has one worker, a fixed 64-record queue and bounded caller waits.
A short write, failed write, saturation or uncertain delivery prevents further
admissions and fails readiness until the controller is restarted. Authorised
physical cancellation and bounded node expiry remain available. A late or partial
log write cannot make an unsuccessful admission successful. A receipt confirms a
complete write to the process log destination; durable storage and access control
remain the runtime/logging operator's responsibility.

Kubernetes auditing must separately use a metadata-only rule for
`tracepreflights`, `traces` and `traces/stream` before broader request/response
rules. See [the audit policy example](STREAM.md). Do not collect request or
response bodies for these resources.

## Quotas and stable reasons

The admission manager owns one frozen policy and performs current authorisation,
identity and quota checks. Nodes consume the resulting bounds and cannot widen
them. Requests exceeding a limit are rejected rather than silently clamped.

| Concurrency scope | Default | Hard ceiling |
| --- | ---: | ---: |
| Authenticated actor | 1 | 2 |
| Namespace | 2 | 4 |
| Node | 1 | 2 |
| Controller installation | 32 | 64 |

Pending reservations and uncertain cleanup continue consuming quota. The single
controller uses `Recreate`; this is not an HA or distributed-quota claim.
Duration, event, output, map and path ceilings remain those of the shared trace
contract. Public denials retain generic Kubernetes status reasons: 403 Forbidden,
429 TooManyRequests, 409 Conflict for target changes, and 503 ServiceUnavailable.
Detailed raw dependency errors and another tenant's identity are not returned.
Audit reasons are fixed categories such as `denied`, `capacity`, `target_changed`
and `unavailable`.

## Retention and deletion policy

The server stores no trace results or event archive. `resultRetentionNanos` is
therefore fixed at zero in audit limits; no configuration can enable a server
result store. One ephemeral stream carries bounded events and its terminal
summary. Closing the stream releases its result buffers; this is logical release,
not a secure-erasure claim about process memory.

- Pending admissions expire within 15 seconds. Active sessions cannot exceed
  five minutes, with at most two seconds for terminal drain.
- Confirmed cleanup removes the admission and its reservation. Unknown cleanup
  retains bounded identity/ownership metadata and quota until it is resolved;
  it does not retain trace payloads or allow a replacement session to use that
  quota.
- The CLI releases its in-memory result when the command exits. The TUI retains
  one bounded terminal result until another session replaces it or the TUI exits.
  Revoked access clears displayed evidence and requests cancellation.
- Explicit local exports are operator-owned files. Server expiry does not delete
  those files or the operator's external logs. Their access, retention and removal
  follow the operator's incident-data policy.

The [compatibility contract](TRACE_COMPATIBILITY.md) keeps retained export schemas
1 and 2 readable without rewriting files. Current writers emit schema 2; its
contract acknowledgement is explicitly unknown when it was not observed. Archive
validation is not provenance verification or an instruction to retain or delete data.

The API provides no completed-result retrieval or replay after expiry. Removal
and rollback disable new sessions and preserve bounded cleanup of active ones;
uncertain cleanup must be resolved before replacing controller/node identities.


## Local functional verification

On 30 September 2026, a separate development installation was exercised on a
two-node kind Kubernetes 1.37 cluster with LinuxKit arm64 and Cilium enforcement:

- A wrong audit-key digest failed the metadata pre-install hook before any
  trace deployments were created. The same prerequisites with the correct digest
  installed successfully. Helm deleted the failed hook under its configured
  deletion policy; its log was unavailable after failure.
- Stateless preflight attached no BPF state. Expiry, cancellation and an
  explicitly consented fixture-path session produced three immutable lifetimes.
  An external census confirmed worker and captured BPF object removal.
- Four concurrent additional requests received generic 429 reasons while the
  fixture session held capacity. An authenticated endpoint-only account received
  a generic 403 without workload details.
- All three sessions' actor, namespace, target and admission HMAC references were
  independently recomputed using fixture inventory and the temporary key.
  Aggregation advertised no UID header, so the authenticated actor UID was empty.
  Each session had one terminal outcome, a confirmed cleanup record and unchanged
  limits with zero result retention. All 32 audit records were below 2 KiB.
- The authorised path stream returned exactly 100 fixture file events and ended
  at the event ceiling. Observed paths and target lifetime identifiers were absent
  from API/node logs and audit records; the raw stream was not saved.
- Completed or cancelled admissions returned 404 on both reads and stream replay.
  Redacted CLI exports omitted target identities and observed fixture paths.

The test installation, fixtures, grants, temporary seccomp file and local private
keys were removed, and the retained prototype APIService was restored Available.
Unit/race tests additionally cover all four quota scopes, bounded audit queues,
failed/short/blocked writes, denied disclosure, cancellation after audit failure,
key rotation and admission expiry while waiting for a receipt. These checks do
not establish resource qualification, another kernel or managed-provider support.
