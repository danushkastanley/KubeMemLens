# Candidate SDK dependency review

Date: 13 September 2026. Scope: Inspektor Gadget v0.56.0 at
`e5a2855f270ca6557f4bd7e4fabaddf6760d8f50`, using its unchanged Go module graph.

This review covers `pkg/gadget-context`, `pkg/operators/ebpf`,
`pkg/operators/oci-handler` and `pkg/runtime/local` as a proposed SDK import set.
These packages are not dependencies of the standard KubeMemLens commands.
The implemented internal trace contract uses only the Go standard library.

The Linux/amd64 source scan reported the following advisory groups. Scanning
exported library entry points is broader than scanning a particular worker
binary; it does not establish which paths a constrained worker can execute.

| Advisory | Scan result | Source assessment and next gate |
| --- | --- | --- |
| [GO-2026-5064 / GHSA-33vj-92qq-66hc](https://github.com/containerd/containerd/security/advisories/GHSA-33vj-92qq-66hc) | Called-symbol traces under containerd v1.7.33; no fixed v1 version listed in the Go record | The primary advisory identifies CDI metadata handling in CRI checkpoint restoration and lists affected containerd/v2 ranges. Check the actual worker call graph and preserve the discrepancy for review |
| [GO-2026-5338 / GHSA-cvxm-645q-p574](https://github.com/containerd/containerd/security/advisories/GHSA-cvxm-645q-p574) | Called-symbol traces including package initialisation | The primary advisory identifies CRI checkpoint import tag poisoning in containerd/v2. Do not infer an exposed restore service from an init trace |
| [GO-2026-5622 / GHSA-rgh6-rfwx-v388](https://github.com/containerd/containerd/security/advisories/GHSA-rgh6-rfwx-v388) | Called-symbol traces including package initialisation | The primary advisory identifies CRI checkpoint log restoration in containerd/v2. The candidate must expose no CRI restore or runtime socket path |
| [GO-2026-5841](https://pkg.go.dev/vuln/GO-2026-5841) | Imported/required dependency; no called-symbol trace | Review `github.com/klauspost/compress/s2`; the scan lists v1.18.7 as fixed |
| [GO-2026-5932](https://pkg.go.dev/vuln/GO-2026-5932) | Imported/required dependency; no called-symbol trace | The obsolete OpenPGP package must not become a signature-verification path for the worker |
| [GO-2026-6354](https://pkg.go.dev/vuln/GO-2026-6354), [GO-2026-6355](https://pkg.go.dev/vuln/GO-2026-6355) | Imported/required dependency; no called-symbol trace | The scan lists golang.org/x/crypto v0.56.0 as fixed. Review an explicit worker-only dependency pin before compiling that worker |

The three containerd Go records omit affected-symbol lists and mark the v1
module affected from its beginning, whereas the primary GitHub advisories list
v2.1/v2.2/v2.3 ranges. The inspected SDK graph imports containerd v1.7.33,
contains no containerd/v2 package, and includes only `pkg/cri/constants` from
containerd's CRI package subtree. This evidence narrows the likely applicability;
it is not a blanket waiver or an independent security acceptance.

Before accepting the runtime candidate, scan the actual worker entry point,
review the affected code paths, apply any justified compatible dependency fixes
in the isolated worker module and retain a finding decision per advisory. Do not
suppress the scan, alter the standard product's dependencies or call the SDK
vulnerability-free. The [engine contract](ENGINE_CONTRACT.md) and independent
security gate remain authoritative.
