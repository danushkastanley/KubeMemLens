# Development chart local verification

Date: 30 September 2026. This report covers functional packaging on one local
LinuxKit arm64 profile with Kubernetes 1.37 and Cilium 1.20.1. It does not establish
resource qualification, managed-provider compatibility or release readiness.

The separate chart remains disabled by default and offers only explicitly
acknowledged unqualified development use. See the
[installation runbook](CHART_INSTALLATION.md) and
[packaging threat model](CHART_THREAT_MODEL.md).

## Frozen runtime

| Item | SHA-256 |
| --- | --- |
| Local image index | `f0f3c6c9aeedcd63ffcd34ec7318845ac88783a2b8b1791e2abb29011a9f5dbd` |
| Launcher | `1c953b7988bf4568bfac9f4ab0785a1c5b21f0b098b4736742cb31ae49903368` |
| Accepted base image | `e82b3f1163fc746a9f002a40adc49b0d4699b5c05a346a0960c720450d92777d` |
| Accepted arm64 worker | `dc997696118e2d27b41874fb8d64ccb0eccee5ee96204f5f26216348f39dd1c0` |
| Installation policy bytes | `223c8c992531d8894c5f3e00289a116ecb7a2b3da24ac3434d5cac97ef8c4d62` |
| Administrative ownership census | `ec0d03526a976f97f04ee47f73ff7201979d1c4bc1010bfe327e21dc52999100` |

The image adds only the launcher to the accepted base layers. Signed workers,
programme objects and installation policy were unchanged. The test image is local;
this record does not imply a published registry artefact.

## Checks completed

- Strict Helm lint, zero-resource default, enabled manifest contracts and negative
  schema/profile tests. The standard chart remains independent.
- Exact policy digest, strict installation/registry decoding, TLS chain/name/key/
  usage/pin validation, node identity checks and runtime drift tests.
- Complete trace-module race tests, module verification and vet; Linux amd64 and
  arm64 builds; 12 Kubernetes renderer and 32 qualification-harness regressions.
  The vulnerability scan reported no reachable vulnerable symbols, while retaining
  its separate imported-package and module advisory observations.
- Real pre-install metadata/trust Jobs rejected a changed Node UID, changed policy
  digest and revoked Node-read permission. A stale-UID Helm installation created
  no API or node workload. Valid metadata and bounded non-attaching host hooks
  completed during installation and upgrade.
- Real Helm install, concurrency configuration upgrade, same-build configuration
  rollback and uninstall. Tenant create/get/delete checks and an authenticated
  aggregated API read were denied without an operator binding.
- Fresh probe Pods on separate nodes verified same-release API-to-node access,
  denied tenant-to-node ingress and denied node-initiated egress, with reachable
  control endpoints before and after the denied paths.
- Bounded 30-second file traces against an idle, non-root 64 MiB synthetic Pod
  witnessed one active accepted worker with five links, five programmes and seven
  maps. During upgrade, captured objects were absent and the original parent had
  exited within 8.41 seconds of the Helm invocation; the replacement node was idle.
  A subsequent trace was admitted and attached through the replaced API.
- Active uninstall removed captured objects and the original parent within
  0.76 seconds of the Helm invocation. Every rendered chart resource and hook
  Job/configuration/network policy was then confirmed absent.

Teardown observations used fresh Kubernetes/CRI identity, the exact parent binary
hash and process lifetime, accepted worker identity and selected fixture cgroup.
The separate administrative census read only owned worker descriptors and opened
only their captured BPF IDs. No global object enumeration, runtime privilege
expansion or deletion of unrelated BPF objects was used.

## Retained failures and limits

Exploratory runs remain in local evidence. One census invocation rejected
non-canonical JSON; corrected reads confirmed cleanup, but its initial measurement
was inconclusive. An immediate post-upgrade response could not be decoded. A
readiness probe then incorrectly added a query parameter through kubectl's request
timeout option; the trace API deliberately rejects query parameters. Corrected
read-only discovery and subsequent admission passed without relaxing that contract.

An initial uninstall assertion checked process exit immediately after Helm returned.
The final check waited for both process exit and zero captured objects within the
existing ten-second observation bound. A probe that relabelled an existing Pod did
not establish egress denial; the passing isolation test used fresh Pods with their
intended identities. Neither earlier result was counted as a pass.

The original prototype APIService was backed up, temporarily replaced and restored
Available after each run. Test namespaces, grants, TLS Secrets, added baseline
seccomp files and local private keys were removed. No OOM attempt, cloud execution
or publication occurred. No cross-version upgrade, amd64 kernel execution,
performance-budget pass or independent review is claimed.
