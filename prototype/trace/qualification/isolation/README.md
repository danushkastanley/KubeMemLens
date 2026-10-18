# Tenant-isolation qualification

These test helpers exercise the optional trace API through Kubernetes aggregation.
They are not release binaries and do not establish provider or resource qualification.
The administrator supplies an explicitly owned installation and two restricted
fixture namespaces. No component discovers or deletes unrelated resources.

`transport.py` accepts verified loopback HTTPS, explicit fixture bearer tokens and
only the required Pod, preflight and trace routes. It bounds request/response sizes,
disables redirects and retries, rejects ambiguous JSON and retains raw responses only
in memory. `access.py` exercises tenant, colleague, administrator and unbound identities
with positive owner controls before and after adversarial probes.

`fixtures.py` constructs pinned-image Pods with 64 MiB memory, 500m CPU, a 30-minute
active deadline, no token, dropped capabilities, read-only roots and bounded emptyDir.
Use the [fixed-seed workload](../filecache/README.md) as the independent reference.
Execute workload operations against the frozen full CRI container ID after checking
the Pod UID, container lifetime, node and executable hash. A name alone cannot select
a fixture after recreation. Prepare files before starting a measured trace.

For provider campaigns, `fixtures.job` puts the same bounded workload in a
single-child Job with no retries. Kubernetes creates the child and its controller
reference; do not fabricate an owner reference on a standalone Pod. Resolve the
actual child against the created Job UID, then retain its original Pod UID and
container identity for every operation. Pass the observed two child names as
`AccessCases(..., pods={"a": first_name, "b": second_name})`; the helper retains
its distinct existing/absent probes and never assumes a generated child is named
`target`. The orchestration layer still guards the corresponding Pod/Job UIDs. Provider images remain digest-pinned and
may be pulled when absent. Node names accept DNS subdomains, including the dotted
hostnames used by EKS. This fixture preparation does not qualify a provider.

`session.py` owns one hash-pinned native `isolationclient` process and private evidence
files. The client uses production target selection, preflight, session validation and
redacted reporting. It accepts only local, inline-CA ServiceAccount credentials; exec
plugins, impersonation and alternative credential mechanisms are rejected. Its private
progress channel supplies the admission handle and metadata-ready signal for active
probes. It never writes event rows or file paths to a report.

`observations.py` compares complete production reports to independent I/O/residency
receipts. File noise must exercise read/write activity. Cache noise must discard and
reload each peer's own file and verify residency; warm reads alone cannot establish
cache-event isolation. Unknown, lost, sampled or rejected counts cannot pass. Selected
I/O has exact byte/base-page totals; idle targets must have numeric zero observations
and delivery despite verified peer activity.

## Run and evidence requirements

Run unit tests with:

```sh
python3 -m unittest discover -s prototype/trace/qualification/isolation -p 'test_*.py'
(cd prototype/trace && go test -race ./qualification/isolationclient)
```

Build the native test client from the repository root:

```sh
(cd prototype/trace && go build -trimpath -buildvcs=false -o /owned/private/directory/isolation-client ./qualification/isolationclient)
```

An administrative campaign must freeze image, executable, programme, policy, Node
and fixture identities before installation; use real namespace-scoped ServiceAccount
credentials; and verify effective grants rather than assume RBAC has propagated.
Never publish kubeconfigs, tokens, raw responses, private inventories or paths.
Preserve failed attempts separately from subsequent corrected runs.

Before workload I/O, witness metadata plus the selected worker and attached owned
objects with the independent [lifecycle census](../lifecycle/README.md). After every
case, prove zero owned workers/controls and disappearance of captured object IDs.
Do not enumerate or remove global BPF state. The access receipt deliberately reports
`kernelCleanupVerified: false`: HTTP cancellation alone is insufficient evidence.
A nonzero client exit remains a failure/uncertainty receipt even when a validated
expiry report is available; external cleanup evidence must be recorded separately.

Each campaign must restore its original API registration, remove only UID-bound owned
fixtures and grants, remove only hash-matched temporary seccomp profiles, and delete
its local private keys/tokens. Independent review, resource qualification and managed
provider evidence remain separate requirements.


## Bound EKS clients

The default transport and native client remain limited to loopback HTTPS and
`kind-` contexts. The explicit EKS mode uses the separately verified provider
campaign's exact API endpoint and SHA-256 of its decoded serving CA bytes. It
requires context `qualification-eks` and inline-CA, token-only fixture credentials.
It never accepts the operator's AWS exec-plugin kubeconfig as a tenant identity.
It does not discover clusters, grant access, install workloads or qualify EKS.

For Python control probes, instantiate `EKSTransport(binding, actor_config)` from
`eks_transport.py`, where `binding` contains exactly `endpoint` and `caSHA256`,
and `actor_config` is the already pinned synthetic actor's kubeconfig document.
The existing route, body/response bounds, strict JSON, redaction, no-redirect and
no-retry behaviour is shared with the local transport. The default constructor
still rejects remote endpoints.

For the native isolation client, add the same two-field object as `eks` in the
private configuration and set `context` to `qualification-eks`. The helper checks
that the resolved kubeconfig endpoint and inline CA match that binding before
creating Kubernetes clients. Alternate credentials, impersonation, custom
transports, proxy overrides, CA files and insecure TLS remain rejected. All trace
selection, consent, duration, event and report bounds remain unchanged.

Only the campaign controller can supply the approved EKS endpoint/CA after its
AWS account/cluster/Node/boot checks. These client bindings supplement that
controller and effective ServiceAccount/RBAC checks; possession of an endpoint
hash is not execution approval or proof of identity. Keep credentials private and
verify actual kernel/process cleanup separately after every active case.
