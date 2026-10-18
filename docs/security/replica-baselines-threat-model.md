# Replica baseline trust boundary

The authenticated extension API exposes optional `pods/replicas` and
`workloads/replicas` reads only in explicitly configured namespaces. A comparison
contains tenant-sensitive peer names and workload identities. It must not discover
or disclose hidden peers by using cached labels, a broad selector or old permission.

The metadata resolver authorises the named comparison, root workload, each selected
Pod and each owner/revision read. It checks actual owner UIDs and namespace identity,
limits acquisition and never persists Pod labels, environment values, commands or
free-form Event text. The collector projects only this verified selection, joins
exact authenticated Node/Pod/container identities, and excludes incomplete sources.
A second fresh resolution repeats authorisation and compares structured membership
and change evidence before disclosure. A replacement, revocation or intervening
change discards the response. Denied requests do not return peer names or counts.

Queries have fixed semantics, no arbitrary selector or remote URL, one concurrent
comparison, a nine-second deadline, 16 peers, eight containers per Pod and a
256 KiB response limit. The underlying Kubernetes adapter has its own bounded
operation rate, object budget and deadline. Ordinary live reads use a separate gate.
History uses existing bounded retention with one continuity hash/clock per series;
that additional work is enabled only by the replica profile.

The response decoder rejects duplicate keys, unknown fields and oversized or nested
collections. Clients verify selected identity, namespace, timestamps and the report
contract. Validation recomputes median/MAD, range, scores and outlier labels from the
reported named reference values. Terminal-facing caveats reject control characters.
The TUI cancels obsolete reads and clears retained comparison data on revocation.

Residual limits: Kubernetes metadata, Events and cgroup samples are not an atomic
transaction. Revalidation detects observed changes but cannot prove absence of a
brief change between reads. Partial Event retention and missing source fields remain
explicit uncertainty. A compromised authorised producer or API remains a source
trust risk; statistical comparison does not authenticate kernel truth or establish
causation. Permission changes after the final check follow the ordinary request-time
authorisation boundary. The feature performs no automated remediation and changes
no severity. External independent review and managed-provider evidence are separate.
