# Local trace compatibility verification

Verified on 30 September 2026 against a two-node Kubernetes 1.37 kind cluster,
LinuxKit 7.0.12 arm64, containerd 2.3.4 and enforcing Cilium 1.20.1. This is
functional development evidence. Resource thresholds, managed-provider support
and release promotion remain separate, open gates.

## Binaries and scope

The previous development extension is the BPFA-004 image
`sha256:e34bbbf750ab78faa340ca6422f7e407f7c6a937eaa28035cae018682e30be3a`;
its launcher SHA-256 is
`c28edb710f267d293e071eec47405b80e36cfb7e1ef48bb679b5854b9eb0d04d`.
The current compatibility image is
`sha256:8abbff46dd72671b774b0d75e2fc83f8965ae852b8b7230870602f57b849531b`;
its launcher SHA-256 is
`4a3c0df1c3c9757e7bb8b1fb8f235adb1c6a646df3653d3767a9b73e803eb8d3`.
Both retain the same accepted worker/SDK/programme base layers and policy.
Only the launcher layer changes. Neither image was published.

Production client/session/report code was exercised through the native bounded
qualification helper. The previous helper SHA-256 is
`8f4225f08cfb547391b717cfaabde8ec13807af3317a3cc5f4d0009d3f805995`;
the current helper SHA-256 is
`4df19ea0adc48ba0f599e1a0d65b0c58c26fee9e878e9e435f35706a935bf17b`.
The current CLI SHA-256 is
`48195106b4f9c48e4af041bf426a58d6e0a14b0d1f6e741e275b388c00e4a418`.
These are development binaries, not released trace clients.

The actual published `v1.0.0-rc.2` Darwin arm64 archive was also checked against
the GitHub release asset SHA-256
`44de602f1166f888831c8ee6b243bc24b21fbe5d75621ee77ed314fe690534df`.
Its version and sample commands passed; `trace --help` correctly reports that the
command is absent. No released trace compatibility window is claimed. The standard
collector/agent protocol is unchanged by this work.

## Observed matrix

| Pair or boundary | Observed result |
| --- | --- |
| Previous development client → previous extension | Five-second file trace completes with schema-1 export; physical kernel cleanup verified |
| Previous development client → current extension | Same legacy request/response shape accepted; five-second trace completes with schema-1 export and physical cleanup |
| Current client → current extension | Aggregated API forwards contract acknowledgement; schema-3 admission and guarded activation complete; schema-2 export records contract 1 |
| Current CLI → previous extension | Explicit incompatibility at discovery, before trace admission or activation |
| Current schema-3 request → previous extension | Preflight and admission reject with 400; kernel remains idle |
| Current activation marker → previous pending handle | 400; original owner remains admitted with identical expiry; no worker or kernel objects appear; authorised cancellation succeeds |
| Future disjoint offer → current extension | 406 before activation for preflight and admission |
| Current body without negotiation, or previous body with current offer | 406 for both operations |
| Combined ambiguous offer | 400 for both operations |
| Previous client requesting more than the event ceiling | 400; compatibility does not widen policy |

Each successful active case used an idle fixture and observed zero produced,
lost, sampled and rejected events. A separately pinned, identity-bound census
observed the worker, active control and BPF objects during the session, then their
absence. A client expiry/cleanup response alone was never treated as physical
cleanup proof. These cases exercise activation and format compatibility, not
nonzero workload correctness or performance; those have separate test campaigns.

The current bounded reader accepted the three real matrix exports without changing
their bytes (two schema 1, one schema 2), plus 14 existing schema-1 reports covering
prior workload and fault cases. Synthetic golden fixtures separately cover file,
cache and OOM semantics, loss, truncation, cancellation, partial transport,
correlation, maximum-width integers and unsafe future fields. Fuzz tests cover
contract negotiation, requests, frames and retained exports.

## Failure retained and cleanup

The first run completed both client pairs against the current extension, but its
pending-handle downgrade probe selected an older pre-schema-2 baseline by mistake.
That request failed the harness's expected admission contract. The failed run is
retained, not reported as an entirely successful campaign. A fresh second run used
the pinned BPFA-004 extension and passed the missing downgrade checks and previous
binary pair. Current CLI refusal was also observed against the older baseline.

Both runs restored the original APIService, removed the temporary chart,
namespaces, grants, added seccomp profile and local private keys. A final check
confirmed the original deployment identities, matched policy and idle kernel
state. No OOM pressure, cloud resources, publication or independent-review claims
are included in this evidence.

See [the compatibility contract](TRACE_COMPATIBILITY.md) and
[prior isolation verification](ISOLATION_LOCAL_VERIFICATION.md). Private local
receipts contain deployment identities and remain outside the public repository.
