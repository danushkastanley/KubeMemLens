# Incident sessions

Development builds provide explicit, owner-scoped incident records in collector
memory. A session links private annotations, Pod memory captures, comparisons,
history/change-marker evidence and explicit operator-supplied trace references.
This work is not a release or managed-provider qualification.

## Enable and authorise

Use matching collector and CLI builds. The namespaces must already exist:

```yaml
incidentSessions:
  enabled: true
  namespaces: [team-a]
```

Apply through the installation's normal Helm upgrade. The feature is disabled by
default and requires the authenticated collector. It adds no Linux capability,
host mount, database, remote provider or automatic remediation. Standalone
collectors use `--incident-sessions=true --incident-session-namespaces=team-a`.
Empty, duplicate and wildcard scopes are rejected.

The chart grants the collector `get` on the configured Namespace names and on
Pods within those namespaces. Namespace identity is checked on every session
operation. It creates three **unbound** ClusterRoles; bind them using RoleBindings
inside each approved namespace:

| Role | Permission |
| --- | --- |
| `kube-memlens-incident-creator` | Explicit session creation |
| `kube-memlens-incident-operator` | Owned session status/actions/deletion, sanitised export and named Pod capture reads |
| `kube-memlens-incident-exporter` | Full authorised export, including private annotations and capture bytes |

For example, an authorised administrator can grant the first two roles:

```bash
kubectl -n team-a create rolebinding incident-create \
  --clusterrole=kube-memlens-incident-creator --group=team-a-memory-operators
kubectl -n team-a create rolebinding incident-operate \
  --clusterrole=kube-memlens-incident-operator --group=team-a-memory-operators
```

Grant the exporter role separately to identities allowed to receive private
records. CLI/TUI comparisons require it to read the retained source captures.
Existing viewer roles do not gain session permissions. TUI browsing still needs
its normal namespace-viewer permissions. Shared namespace RBAC does not provide
collaboration: a different authenticated actor cannot read another actor's session.

Markers additionally require configured [memory history](memory-history.md),
`memoryHistory.markers: true`, and the existing history/marker viewer permissions.
Their source namespaces must include the selected namespace. Disabled, denied,
missing, unsupported and partial evidence remain explicit in the timeline.

## CLI workflow

Use the authenticated Kubernetes connection; legacy HTTP/proxy and restricted-mode
connections cannot mutate session records. Keep the returned session ID:

```bash
kubectl memlens session start -n team-a -o json
kubectl memlens session annotate SESSION_ID -n team-a --note-file decision.txt
kubectl memlens session capture SESSION_ID app-0 -n team-a
kubectl memlens session markers SESSION_ID app-0 -n team-a --source local
kubectl memlens session show SESSION_ID -n team-a
kubectl memlens session compare SESSION_ID evidence-1 evidence-2 -n team-a
kubectl memlens session status SESSION_ID -n team-a
kubectl memlens session close SESSION_ID -n team-a
```

Replace `SESSION_ID` and the example Pod with real values. `show` lists aliases
local to that export; `compare` also accepts exact digests. Compare like domains:
deep Pod schemas 1/2 can cross their supported schema transition; paired schema-6
history captures preserve source, metric, target and clock differences. A Pod
replacement does not establish memory continuity. Collection failures become
visible gap entries rather than invented evidence.

Annotations accept a file or `--note-file -` for stdin, with at most one terminal
line ending removed. They must contain valid, single-line UTF-8, without control or
hidden formatting characters, and fit 512 bytes. Input is not echoed. Avoid putting
credentials in notes; review the logging boundary below before handling sensitive
operational data.

```bash
kubectl memlens session export SESSION_ID -n team-a -o timeline.json
kubectl memlens session export SESSION_ID -n team-a \
  --include-sensitive -o private-session.json
kubectl memlens session replay private-session.json
kubectl memlens session replay private-session.json --include-sensitive
kubectl memlens session compare-export private-session.json evidence-1 evidence-2
kubectl memlens session delete SESSION_ID -n team-a
```

Exports default to an allow-listed sanitised form: local aliases, action/source
labels, times, schema references and gaps. It excludes real identities, digests,
paths, annotations and capture bytes. Full export requires explicit permission
and `--include-sensitive`. Files use mode `0600` and are never silently replaced;
CLI replacement requires `--overwrite`. Offline replay sanitises by default and
contacts no cluster. Export files persist until their owner deletes them.

A timeout or malformed mutation acknowledgement may mean the action completed.
The client does not retry automatically. Inspect a known session ID before a
manual retry. An unconfirmed creation can consume quota until expiry if its ID
was not received. Export files are evidence, not proof of authenticity or authority
to restore a live session; there is no session-restore endpoint.

## Trace references

Collect a bounded report through the separately installed trace extension, using
its own trace permissions and [export workflow](ebpf/CLIENT_WORKFLOWS.md). Keep
the exact report file. Referencing it does not start another trace or give the
collector tracing permissions.

```sh
kubectl memlens session trace-reference SESSION_ID -n team-a \
  --report trace-report.json --confirm-reference
kubectl memlens session export SESSION_ID -n team-a \
  --include-sensitive -o private-session.json
kubectl memlens session verify-trace private-session.json trace-1 \
  --report trace-report.json
```

The client validates the report and hashes its exact bytes, including whitespace.
It sends only a bounded typed reference: schema/contract information, digests,
timing, outcome, loss and cleanup evidence. No report body, caveat text, local path
or URL is sent. The server authenticates the submitting operator and checks the
reference's shape and limits; it does not authenticate the original measurement
or verify the operator's local file. Report target aliases cannot prove its
original namespace or Pod. Both exports identify this as operator-supplied data.

Full exports retain the private reference; sanitised exports use `trace-1` aliases
and retain uncertainty while omitting digests and numeric counters. `verify-trace`
contacts no cluster and checks exact file agreement against an authorised export.
Success establishes matching bytes, not authenticity, qualified tracing support
or a causal link to neighbouring memory evidence. The collector cannot retrieve
the report later; deleting the local file makes that verification unavailable.

Only named regular reports of at most 32 KiB and supported report schemas 1/2 are
accepted by the CLI. Consent is required before reading/transmitting a reference.
The API continues to accept at most 4 KiB per request; it accepts no raw-report
upload. A completed trace can still have incomplete coverage or unconfirmed
cleanup. Those states remain visible in the timeline.

## TUI

Press **I**, or **a** then **i**, from a namespace/Pod selection. Opening a panel
creates no session. Use **n** to start, **a** to attach an existing ID, **r** to
refresh, **t** for a masked annotation, **c** to capture, **m/M** for local/Prometheus
markers, **x** to compare aliases, **z** to close, **e/E** for sanitised/full export,
and **d** for confirmed deletion. **Esc** discards an editor or closes the panel.

For a terminal trace in the same namespace, **T** selects a new local report
destination. Type **attach** at the separate confirmation step. The TUI saves that
exact report privately before sending its typed reference, so it can be verified
later. Existing files are not replaced. If the server action fails or its outcome
is uncertain, the local report remains and no automatic retry is sent. A changed,
revoked, running or differently scoped trace cannot be attached through this flow.

The panel retains the existing reader's cluster and narrows its namespace scope.
It never silently reloads a different kubeconfig current-context. Namespace/source
changes, revocation and expiry clear local evidence. Cancelled/obsolete results
cannot repopulate the panel, but interruption cannot promise server-side rollback.
Closing the panel alone leaves the session available until expiry or deletion.

## Limits and lifetime

Defaults are one hour of non-renewing retention, 64 sessions globally, eight per
namespace identity, 128 timeline entries and 256 KiB of encoded state per session.
The store's hard retention ceiling is four hours; the supported chart/CLI use the
one-hour default. Each entry is at most 4 KiB. Up to 16 unique captures, each at
most 64 KiB, share the session byte limit, including base64 encoding in full export.
These are encoded-data limits, not a claim about exact process heap usage.
The existing 8,192-token decoder ceiling also bounds retained state. A timeline
with larger trace references can reach capacity before 128 entries or 256 KiB;
capacity checks reserve a readable closing entry without raising those ceilings.

Capture bytes and their timeline reference are committed together. Rejected
capacity writes preserve prior evidence, set `limitReached` and reserve space to
close. Required comparison/marker gaps are atomic with their action. Closing stops
new entries without extending expiry. Deletion, expiry and collector shutdown
release server state. A collector restart, rollout or crash loses live sessions;
there is no persistent-volume recovery or multi-replica session routing.

The current retained capture path covers namespace-scoped deep Pod evidence
(schemas 1/2) and Pod history/markers (schema 6). It does not accept arbitrary file
uploads, URLs, application logs, Node/volume/topology captures or raw trace events.
Other product capture commands keep their existing separate contracts.

## Logging, rollout and rollback

The collector does not log annotation or capture payloads. Cluster audit and
proxy configurations are a separate boundary. Kubernetes audit levels can include
request/response bodies; use a matching `Metadata` rule before broader body-level
rules where policy is operator-controlled, and verify managed-provider behaviour.
See the [Kubernetes audit policy guidance](https://kubernetes.io/docs/tasks/debug/debug-cluster/audit/#audit-policy).
Do not infer audit-log privacy from TLS or the sanitised export setting.

Before a collector upgrade, rollback or disabling the profile:

1. Stop participating mutation clients and remove their incident-creator bindings.
   Retain operator/exporter bindings while draining. Confirm no other grants still
   allow creation; cluster-admin or broader grants are not restricted by this step.
2. Export required records through their original authorised identities. Close
   them, then delete them or let their recorded expiry elapse.
3. Only then change the collector image or disable `incidentSessions` and remove
   its namespace list. A restart cannot drain or recover the old in-memory store.
4. Remove obsolete user RoleBindings separately. Helm never creates them.

Timelines without trace references retain the existing schema-1 encoding.
Trace-bearing timelines use schema 2; current readers accept both. A trace entry
cannot be relabelled as schema 1, and schema 2 requires a trace reference. Unknown
versions, malformed privacy declarations and contradictory provenance are rejected.
The live action/status API remains version 1; old development clients cannot read
the new trace-bearing export or unknown latest-entry kind. Use the matching current
client for that workflow. Existing capture and trace-report schemas remain distinct.
The [local functional report](qualification-results/incident-sessions-local-2026-09-30.md)
records real expiry, published-baseline upgrade and retained-format replay checks.
Managed-provider qualification remains separate. See the
[threat model](security/incident-sessions-threat-model.md).

## Repeatable live verification

Use [`hack/verify_incident_sessions.py`](../hack/verify_incident_sessions.py) with
an existing test deployment and a running fixture Pod. Supply two different
identities with the three session roles in the same namespace. The owner also
needs the history and marker viewer roles when testing enabled markers.

```sh
python3 hack/verify_incident_sessions.py \
  --binary ./bin/kubectl-memlens \
  --owner-kubeconfig /private/owner.kubeconfig --owner-context fixture \
  --peer-kubeconfig /private/peer.kubeconfig --peer-context fixture \
  --namespace fixture --pod sample --markers local \
  --output /private/new-session-verification
```

The output directory must not already exist. The harness creates at most two
sessions, uses a synthetic annotation, retains private files with mode `0600`,
and deletes its sessions on success or failure. An unconfirmed mutation is not
retried; a transport error cannot satisfy a denial or deletion assertion.
Review `results.json` for cleanup failures. If creation lost its acknowledgement,
the unidentified session remains bounded by normal retention.

Use `--markers disabled` when the history/marker profile is disabled. The local
marker case requires actual retained Kubernetes markers; a gap alone cannot
satisfy it. This walkthrough covers the current live API, capture, comparison,
exports, owner isolation, close and deletion. Add `--trace-report /private/report.json`
to explicitly attach a reference to an existing report, check the sanitised
projection, deny a different owner's attachment, and verify the original and
changed file bytes offline. This does not start a new trace or authenticate the
report's source. It does not establish expiry over wall-clock time,
version-upgrade compatibility or EKS support.
Exported files persist independently of server deletion; remove them according
to the operator's evidence-retention policy.
