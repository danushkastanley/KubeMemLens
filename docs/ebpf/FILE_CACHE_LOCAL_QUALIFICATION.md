# File/cache local qualification

On 15 September 2026, the accepted BPF-005 candidate was exercised through the
production admission API and binding-node service in an owned two-node local
kind cluster. The tested worker ran on arm64 LinuxKit 7.0.12 with Kubernetes
1.37.0 and Cilium 1.20.1. These results cover this environment only.

The accepted engine digest is
`sha256:d3d549b2f8846badba517c3ab52e86d5b3d05aabab85e5d3fde89a25627cd61c`;
the programme index is
`sha256:09633c214bef977fb5abf4471ff03ed0767ee6cea65b40a58f15b014811910b3`.
Both identities were checked against stream metadata for every recorded case.
The local image and immutable public acceptance policy matched the reviewed
candidate. The worker used the existing BPF/PERFMON profile.

## Observed results

The [fixed-seed workload](../../prototype/trace/qualification/filecache/README.md)
verified every byte of each 8 MiB read and measured page residency independently.
Default output contained metadata and aggregates only.

| Case | Result |
| --- | --- |
| Warm read | 8 MiB verified with all 2,048 pages resident; file activity observed, zero cache observations |
| Warm write | 8 MiB rewritten and synced; file activity observed, zero cache observations |
| Cold read | Zero resident pages after file-scoped advice, 2,048 afterwards; cache trace reported 2,048 removed and 2,048 added base pages |
| Non-selected noise | Ten Pods across two namespaces each read and wrote 32 MiB; idle selected target produced zero file/cache observations and measured zero loss/rejections |
| Selected I/O under noise | File counts and cache addition/removal totals matched their corresponding baseline cases |
| Confirmed paths | 128 fixture-read paths matched the synthetic file; longest delivered path was 50 UTF-8 bytes under the admitted 64-byte ceiling |
| Eight-byte path budget | All 147 candidates were explicitly rejected; no path or event was delivered |
| Eight-observation event ceiling | Exactly eight aggregate observations, `event_limit` termination and unknown terminal engine counters; no fabricated zero counts |
| Ring saturation | Pausing only the owned worker for 1.5 seconds during 32 MiB read/write activity produced 1,043 candidates, 468 observations and 575 reported losses |

Normal-expiry cases preserved measured terminal counts and overlapping cgroup
sampling windows. The saturated-ring case remained partial and reconciled
produced observations with recorded losses. Existing hook blind spots keep even
loss-free file/cache results partial. These measurements do not establish cache
hits, complete filesystem coverage, shared-page ownership or complete memcg
accounting.

Every case collected owned worker descriptor identities during attachment and
checked that those captured maps, programmes and links could no longer be opened
after completion. No worker remained. This was an owned-object census, not global
BPF enumeration. All default runs retained no raw events; consented path comparisons
were performed in memory and only counts and lengths were retained.

## Remaining qualification

Native Linux root race/coverage and vet/build checks passed, along with the
optional module race/vet and Linux amd64/arm64 builds, the nested worker checks,
repository contracts and Kubernetes policy tests. The ticket review/PR sequence
remains required. Other architectures,
managed providers, performance ceilings, the full failure/lifecycle matrix and
independent security review retain their separate gates. EKS testing is deferred
until all phases finish. Standard agent, chart and release artefacts remain
outside this optional prototype's installation scope.
