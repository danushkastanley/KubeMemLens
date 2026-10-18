# Local Node topology qualification — 29 September 2026

The optional topology profile passed the local functional checks recorded in
[summary.json](summary.json). [source.json](source.json) binds production inputs
built from the recorded base commit plus this implementation. Image identities
are local build identities, not published registry references.

## Source verification

An isolated, networkless ARM64 Linux 6.18.54 VM exposed two 768 MiB NUMA domains.
The actual production reader and analysis ran at six controlled stages: baseline,
256 MiB bound to domain 1, a reserved but unfaulted 2 MiB huge page, that page in
use, a scoped HugeTLB limit failure, and release. Kernel source readings matched
pool counts, cgroup charges/failures and NUMA totals. The analysis detected the
held uneven distribution and returned within threshold after release. An
outstanding reservation stayed separate from in-use bytes. The scoped failure
was SIGBUS, not a host/global OOM. The VM was powered off and its container removed.

The existing two-Node kind 1.37.0 cluster ran the actual producer, collector,
authenticated API, CLI and TUI. LinuxKit 7.0.12 exposed no NUMA sysfs tree, four
zero HugeTLB pools, and finite cgroup limits. Missing NUMA remained unsupported;
reported zeros remained present. An initially over-strict limit-alignment check
was corrected after observing the finite value 9223372036854771712. The final
reader preserves limit bytes exactly while still rejecting misaligned usage.

Ordinary Node available, usage, working set, RSS and fault counters matched the
kubelet Summary at the exact same source timestamp. No ordinary value was adjusted
by the topology domain. Positive hugepage allocation and Kubernetes accounting
were exercised in their respective VM and kind profiles; this is not a live
Kubernetes eviction qualification.

## Disclosure and operator paths

Live checks verified authenticated publication, named-Node access, denial for
ordinary Node viewers, denial for another Node, omission for schema 6, and denial
of Pod reads for the Node-only test identities. Schema-7 captures retained source
values and caveats, used local aliases and private file mode, and replayed offline.
A denied overwrite preserved the existing file. Ordinary captures remained schema 4.

Actual terminal journeys at 80×24 and 160×35 opened Node detail, paused polling,
scrolled through NUMA and HugeTLB sources, exposed page and cgroup failure counts,
reached the explicit capture command, returned to the first page and exited.
The harness accounted for wrapped lines and incremental terminal updates.

Removing the topology grant caused a fresh denial. Disabling the profile removed
discovery, the unbound viewer role, topology flags and host mounts; an authorised
CLI topology request returned not found, while the ordinary Node endpoint still
returned successfully. The Helm release, owned namespace, roles, bindings and
APIService were removed. Temporary kubelet certificates stayed within the owned
Nodes; original configuration hashes were restored and fixture keys removed.
Both original Nodes remained Ready. The reusable cluster was retained.

## Automated checks

Host tests, coverage, the full race suite, vet, vulnerability scanning and builds
passed. The scanner reported zero reachable vulnerabilities; non-called imported
package/module findings were not suppressed. Native Linux checked the optional
worker's race/vet and cross-build paths, and topology/source/admission/capture/UI
packages. A final Linux regression covered the finite-limit correction.

Chart tests verified opt-in mounts, dropped capabilities, unchanged ordinary
permissions, the unbound named-read role, and rejection of invalid profile/root
configuration. Provider, terminal, release and community contracts passed against
an isolated public-source candidate commit, because exact-source checks correctly
reject an uncommitted chart against its older base commit. No contract was weakened.

These are our local results. Managed-provider qualification remains separate.
