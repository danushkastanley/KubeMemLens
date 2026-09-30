# Development chart trust boundaries

This packaging extends the existing [file/cache threat model](FILE_CACHE_THREAT_MODEL.md)
and [admission boundary](ADMISSION.md). It does not qualify the candidate or change
its signed programmes, worker limits, trace permissions or public stream contract.

| Boundary or threat | Control and remaining limit |
| --- | --- |
| Tenant enables host access through a standard installation | The separate chart renders no resources by default; the standard chart has no dependency on it. Enabling the development profile requires explicit acknowledgement and administrator prerequisites. |
| Values widen privileges or retarget nodes | Strict schema, bounded distinct node identities, fixed arguments/mounts/capabilities, exact Node RBAC and pinned image digest. There is no arbitrary Pod specification or argument escape hatch. |
| Policy replacement under an existing name | Administrator-owned immutable ConfigMap plus a mandatory file digest. Both processes hash the exact bounded bytes they parse. Signed worker and programme acceptance still applies. |
| Node replacement or kernel/runtime drift | Fresh Node reads match UID, architecture, kernel and containerd versions during admission, stream revalidation and OOM context reads. Startup checks the actual kernel. Kubernetes node metadata remains an administrator-controlled assertion, not remote attestation. |
| Counterfeit node service or control peer | Dedicated TLS identities, CA validation and exact leaf pins. Installation checks names, validity, usages and both trust directions. Each node verifies its own private key at startup. |
| Preflight gains durable privileges | Metadata Job runs without capabilities or host mounts and uses an existing exact-Node reader account. Host Jobs get only BPF/PERFMON, the baseline seccomp profile, bounded runtime and read-only mounts; no token or network. They do not attach incident programmes. |
| Failed hooks leave permissions behind | No hook creates an account or cluster grant. Jobs have deadlines, no retries and a finished-job TTL. Administrators inspect residual hook objects after failure. Prerequisites remain explicitly administrator-owned. |
| Tenant connects directly to node service | NetworkPolicy admits only same-release API Pods; mutual TLS and pins remain mandatory. Fresh-pod policy tests exercise allowed and denied paths. Existing connections after label changes are not evidence of fresh-flow isolation. |
| Node opens outbound connections | Node NetworkPolicy denies initiated egress. Reply traffic remains possible. This requires an enforcing CNI; rendering a policy is insufficient evidence. |
| Namespace writer impersonates API labels or replaces trust | Namespace writes are an administrative trust boundary. Keep tenants out of the installation namespace; network labels alone are not authentication. |
| API credential misuse | Separate account with Pod gets, exact Node gets, delegated SubjectAccessReviews and the aggregation authentication reader. Operator role is unbound. Tenant create/read/delete access remains denied unless explicitly assigned. |
| Removal appears complete while kernel state survives | Check captured, ownership-bound BPF IDs independently after active teardown, plus original process termination and chart resource inventory. Helm success and Pod absence alone are insufficient. No global BPF enumeration, unpinning or additional runtime capability is introduced. |

The chart leaves API egress unrestricted for Kubernetes API access, DNS and node
services. It permits API ingress on its TLS port for the aggregation layer. These
are documented deployment boundaries, not claims of complete network isolation.
External Secrets, policy, namespace and preflight permissions require separate
administrative removal. Certificate rotation and node upgrades require new reviewed
values and preflight; rollback must not reinstate stale identities or trust.

Verification combines chart render contracts, strict parser/trust and profile-drift
tests, real aggregation/RBAC checks and bounded local lifecycle tests. Independent
review, resource budgets and provider qualification remain separate and incomplete.
