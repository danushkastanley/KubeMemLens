# Local tenant-isolation verification

Date: 30 September 2026. This records bounded BPFA-004 development tests on the
owned local Kubernetes 1.37 cluster, LinuxKit 7.0.12 arm64, containerd 2.3.4 and
Cilium 1.20.1. It does not establish resource qualification, managed-provider
support, independent review or release readiness. No cross-tenant disclosure,
retargeting or premature quota release was observed in the assessed cases.

## Candidate and method

The optional chart used the existing accepted worker, signed programmes and
BPF/PERFMON profile, with the current admission, client and audit implementation.
The standard agent, chart and release images were unchanged.

| Artefact | SHA-256 |
| --- | --- |
| Local candidate image | `e34bbbf750ab78faa340ca6422f7e407f7c6a937eaa28035cae018682e30be3a` |
| API/node executable | `c28edb710f267d293e071eec47405b80e36cfb7e1ef48bb679b5854b9eb0d04d` |
| Acceptance policy | `223c8c992531d8894c5f3e00289a116ecb7a2b3da24ac3434d5cac97ef8c4d62` |
| Gated fixture image | `4e0103468ac45242cfd3f74e0f46aeb20bd9d3c5b03e8e09a2c2108ff6ea177e` |
| Fixture executable | `602eef04abf622d22f653776ea1ae1a61791536d432e1ced18af6ed3ab5b5f90` |

The [isolation helpers](../../prototype/trace/qualification/isolation/README.md)
use authenticated Kubernetes aggregation with real ServiceAccount tokens. Two
restricted tenant namespaces have separate accounts; a same-namespace colleague,
an administrator granted both tenant roles, and an unbound account are additional
callers. Effective identities and namespace grants were verified before testing.
Administrative fixture operations do not grant the product an authorisation bypass.

Each selected fixture was bound to its Pod UID, full CRI container ID, node,
executable hash and process lifetime. Workload commands used the exact container
ID. Fixtures had 64 MiB memory, 500m CPU, no token, dropped capabilities, read-only
roots, bounded emptyDir and a 30-minute active deadline. No global OOM experiment
or unrelated workload was used.

## Access and disclosure

The final access run recorded 95 HTTP observations: 50 forbidden responses,
33 absent responses, three owner admissions and nine owner reads. Tenant peers
could not discover, preflight or create against the other namespace. Cross-tenant,
colleague, administrator and unbound callers could not read, watch or cancel
another owner's admission. Positive controls verified owner state and deadline
before and after those probes.

During an active confirmed-path trace, all four non-owner callers failed actual
CLI export attempts, created no output file, and left the owner's active state
and deadline unchanged. Administrators could create their own authorised trace
but could not adopt another owner's handle. Redacted reports and routine API/node
logs omitted fixture identities, tokens and the synthetic file path.

## Kernel filtering under peer I/O

Ten non-selected Pods, five per tenant namespace, ran independently verified
fixed-seed I/O. File noise produced 320 MiB reads and 320 MiB writes per case.
Cache noise performed 40 uncached reads totalling 320 MiB; every operation verified
zero resident pages after advice and complete residency after reading.

| Selected workload | Without peer activity | With ten active peers |
| --- | --- | --- |
| Idle file target | Separate idle positive control | Zero produced, delivered, sampled, lost and rejected events |
| 8 MiB cached read | 128 events; exactly 8 MiB requested/completed reads; zero writes | Same byte totals and event counts |
| Idle cache target | Not measured separately | Zero produced, delivered, sampled, lost and rejected events |
| 8 MiB uncached read | 2,048 base pages removed and added | Same exact base-page totals |

All six final file/cache cases reached validated expiry with known zero sampling,
loss and rejection. Cache observations were aggregated without event-row delivery.
The number of cache records differed with folio sizes, so comparisons used exact
base-page totals rather than treating record count as page count. The collector's
partial hook-coverage caveat remained visible.

A selected CRI exec initially added runtime setup I/O to the measured container.
That run failed the exact-byte assertion. The corrected fixture starts before
attachment, waits for a bounded stdin command, emits its reference receipt and
remains alive until tracing finishes. No measured events or bytes were subtracted.
The revised static executable reproduced byte-for-byte and its direct verifier
checked I/O, residency, command rejection, corruption rejection and cleanup.

## Target lifetime, permissions and restart

A live selected-Pod deletion/recreation ended the original session with
`target_changed` and no replacement events. Direct API calls rejected stale Pod
UID, container ID, start time and node selection, plus attempts to supply node or
cgroup authority. Replaying individual old lifetime fields against the replacement
also failed.

The same-name fixture was then recreated on the second reviewed node. Its host
preflight first rejected a missing securityfs mount; the approved read-only local
prerequisite was staged before retrying. The old session stopped without following
the moved fixture, and combining its new lifetime with the old node selection was
rejected. An explicit new selection on the second node reached validated expiry
with zero idle observations and complete physical cleanup.

Removing the exact owner's RoleBinding ended its stream with `authorisation_lost`
and omitted rich aggregates and correlation. Captured kernel state was removed.
Restoring the fixture grant required a fresh effective-permission check.

Killing only the identity-verified API process produced failed, incomplete transport
with no invented summary. Killing the verified node process removed its owned
kernel objects; the restarted node could not confirm predecessor cleanup. The API
retained the uncertain reservation and returned 429 to a fresh admission even
after the replacement was ready. Administrative recovery verified all captured
objects absent and the new node idle before restarting the API. A new admission
then succeeded and was cancelled.

## Network and error observations

Twelve alternating existing/absent handle pairs for each of a cross-tenant caller
and a same-namespace colleague produced matching status bodies after normalising
the caller-supplied handle. Responses carried no protected target identity or
trace counters. These are 48 bounded timing observations, not a statistical proof
of absent timing channels. Admission checks permissions before resolving targets;
read/cancel paths check permissions and ownership before exposing target details.

Removing the exact owned NetworkPolicy ingress allow rule blocked fresh TCP and
exact-lifetime preflight requests. An existing stream still delivered its expiry
summary with known counts, although client transport/cleanup remained incomplete.
That result does not establish interruption of established traffic: Kubernetes
[leaves this behaviour to the network implementation](https://kubernetes.io/docs/concepts/services-networking/network-policies/#networkpolicys-impact-on-existing-connections).
The exact original policy specification and fresh connectivity were restored.

A separate fault installed temporary INPUT/OUTPUT drop rules inside only the
identity-verified test API Pod's network namespace, selecting the owned node Pod
and Service addresses on TCP 9443. Gated selected I/O supplied real stream traffic;
packet counters witnessed drops on the already-established stream. The client
reported failed/incomplete evidence with unknown engine counts. External census
verified physical cleanup by the original admission deadline plus its two-second
drain, without extending the session. Exact original namespace rules and fresh
connectivity were restored. After physical cleanup, an API restart and a bounded
read-only aggregation-readiness check, one fresh admission succeeded and was
cancelled. Post-fault routine/audit logs omitted protected fixture values.
No global firewall, conntrack or BPF state was changed.

## Evidence limits and cleanup

Activation required both validated stream metadata and an independent owned-worker,
control-map and attachment witness. Every assessed case checked selected workers,
excluded children and captured map/programme/link IDs after termination. No global
BPF enumeration or detach operation was used. A client reporting uncertain cleanup
was kept separate from the independent physical-cleanup receipt.

Temporary installations restored the original API registration and removed their
owned namespaces, grants, hash-matched seccomp files and local private keys/tokens.
Failed harness attempts remain in local evidence, including incorrect CLI wording,
zero-restart assumptions and an invalid preflight/policy-restoration test. One
failed partition attempt's immediate post-uninstall census was inconclusive;
a separate subsequent check confirmed all captured objects absent. It is not a
passing partition qualification result.

The two-node campaign's mount removal succeeded but its wrapper misinterpreted
the `mountpoint` not-mounted exit code. Separate UID-bound cleanup removed the
remaining namespaces/grants, verified original sysfs restored and all added files
absent, and rechecked every captured BPF ID. Both the original failure and the
subsequent cleanup proof were retained.

Verification passed the optional module's race tests, vet, module verification,
Linux arm64/amd64 builds, 26 isolation tests, 12 renderer tests and 32 existing
qualification tests. The vulnerability scan found no called-symbol findings;
two imported-package and one required-module finding remained visible. These
results do not waive the separate worker dependency record. The fixed-seed fixture
passed its capability-free, network-disabled live verifier, including gated I/O.

Observed error equality and bounded timing samples cannot establish mathematical
absence of timing channels. Kubernetes authorisation, shared-node scheduling,
resource contention and administration remain part of the deployment boundary.
Independent reviewer/adopter work is excluded from this implementation effort;
no independent endorsement is claimed. Resource qualification remains failed/open,
and EKS validation remains deferred until implementation and local qualification
are complete.
