# ADR 0015: Reject the evaluated eBPF candidate on idle cost

Date: 16 September 2026
Status: no-go for the evaluated R6 candidate; R7 blocked

## Context

[ADR 0001](0001-defer-ebpf-until-security-and-benchmark-gates.md) permits a separate
prototype and retains deferral when its security or performance gates fail.
[ADR 0003](0003-evaluate-inspektor-gadget-behind-kubememlens-admission.md) selected
an engine evaluation, not permission to ship tracing in the standard product.

The candidate implements admission, immutable target binding, ephemeral file/cache
and victim-only OOM output, bounded resources and owned teardown. Local correctness
evidence does not establish acceptable resource cost or independent security review.
The accepted idle limit remains 5 millicores average and 40 MiB working set per node.

Five complete paired local repetitions each measured 15 minutes without the two
optional services and 15 minutes with them idle. All five failed the memory gate:
node peaks were **85.84, 88.14, 88.92, 86.07 and 87.80 MiB**. The colocated API plus
node averaged 6.670–7.346 millicores, also above its predeclared installation budget.
The node-memory failure alone is sufficient to reject this candidate, independent
of how a future deployment places its API.

The [qualification report](../ebpf/IDLE_LOCAL_QUALIFICATION.md) identifies the exact
source, image, worker, programmes, environment, raw windows and replay commands.
Controls retained APIService registration/configuration and existing Cilium; they
do not qualify general node or workload regression. The memory result is absolute
and does not subtract that control. No threshold changed after observation.

## Decision

Record **no-go** for image
`sha256:4a957b8fa80324e9ce8a66d6876766eb4d1ea13ea85a83973b245e4238b741a3`
and its matched candidate. Supported trace profiles: **none**. Do not progress it
to R7, publish its image, add it to the standard chart/agent or describe it as
provider-qualified. Retain the source and sanitised evidence as archived research.

The repeated local result is on LinuxKit 7.0.12 arm64, Kubernetes 1.37.0 and
containerd 2.3.4. Cross-builds do not establish native amd64 or managed performance.
This decision rejects the evaluated design; it does not claim every future eBPF
implementation is impossible.

The node eagerly retains a sealed copy of the 68.99 MiB worker executable. Observed
shmem is consistent with that page-rounded copy and its small policy descriptor.
The accepted ELF's loadable file content alone is 48.53 MiB, so debug stripping
alone cannot make the eager-copy design fit 40 MiB. A different engine/build or
verification/activation lifetime needs a new reviewed candidate and fresh complete
qualification. No such alternative was executed or approved by this decision.

## Alternatives and consequences

Increasing the memory budget, omitting executable backing pages, substituting RSS,
moving costs out of the reported baseline or weakening executable integrity would
not satisfy the accepted gate. Retain the existing limits and integrity controls.

The remaining normal/high-rate/noisy/concurrent/flood performance cases, event loss
and delivery/attach/verifier/scheduler latency, collector-scan regressions and
independent reruns are explicitly unrun under BPF-008. Prior semantic and lifecycle
reports retain their own coverage and limitations. They are not replacements for
performance qualification.

Independent kernel/eBPF, Kubernetes multi-tenancy and non-implementer performance
reviews remain unperformed. Dependency findings and redistribution obligations
are not waived; see the [review packet](../ebpf/QUALIFICATION_REVIEW.md). EKS remains
deferred. A future positive decision must close those gates, not inherit a pass
from this rejection experiment.

## Verification and rollback

The full raw record contains 9,010 samples across ten complete windows. Strict
replay preserves every failing pair and binds candidate metadata, source, raw
timestamps, process lifetimes and owned-state checks. Historical measurement and
later validator identities remain separate. No archived source executes during
export verification.

Final local teardown verified both services idle, all captured owned BPF object
IDs absent and known foreign controls unchanged. The owned APIService was removed
with a UID precondition; both optional services stopped. The dedicated disposable
cluster was removed after its fixture inventory and absence of persistent storage
were checked. Unrelated Docker workloads were preserved.

The standard cgroup product, chart capabilities, release packaging and stored
schemas remain unchanged. No data migration or standard-product rollback is needed.
