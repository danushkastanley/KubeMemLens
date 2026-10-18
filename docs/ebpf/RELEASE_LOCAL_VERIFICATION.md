# Local trace release verification

BPFB-002 local development evidence, 30 September 2026. This records the
implementation's own tests, not an independent review or provider qualification.

## Artefacts and consumer

The previous development set (`0.1.0-dev.1`) used retained signed worker/programme
inputs. The current set (`0.1.0-dev.2`) used freshly reproduced workers and launchers
for Linux amd64/arm64. All six BPF objects reproduced and matched the retained signed
programme build record. Both current OCI exports were byte-identical. No BPF code
was loaded during those builds or consumer verification.

The consumer checked the external local signing authority, exact manifest/subjects,
SPDX/provenance relationships, complete file inventories, two-platform image graph,
engine/programme signatures and image copies, chart defaults and Helm renders.
Seven earlier real signed-bundle cases covered valid verification before/after
probes and wrong-key, subject, signature, re-signed SBOM and provenance mismatches.
The fresh current set also passed the full consumer. Development signatures do
not establish GitHub release authority or installation acceptance.

## Active lifecycle and coexistence

The bounded run used the existing two-node local kind cluster: Kubernetes 1.37,
LinuxKit 7.0.12 arm64, containerd 2.3.4 and Cilium 1.20.1. Tracing and restricted
fixture Pods ran only on the selected worker. A separate standard reference chart
provided real memory API reads throughout the test.

Passed:

- A deliberately mismatched target Node UID blocked a real Helm upgrade before
  changing the existing service deployment identities or policy.
- Upgrade to the current verified image/policy and guarded rollback to the previous
  verified package removed the old parent process and all captured BPF identities.
- Old admission identifiers could not activate on replacement servers; new requests
  used the selected engine identity.
- Disable removed trace workloads and registration, cleaned active kernel state and
  refused new admission. Re-enabling reran preflight successfully.
- Uninstall removed every chart resource and hook Job, ConfigMap and NetworkPolicy,
  together with the captured kernel state.
- After each action, standard agent/collector process and controller identities
  remained unchanged and a fresh real fixture Pod snapshot was returned.

Four files traces used the existing 30-second bound. Cleanup was observed 7.612 s
after admission started for upgrade, 7.910 s for rollback, 2.246 s for disable and
1.506 s for uninstall. These are functional observations, not performance percentiles
or resource-budget qualification. No OOM workload was used.

The first run's disable observation was unproven: its harness stopped when Helm
returned before witnessing asynchronous deletion. Its finalizer completed cleanup.
The corrected run continued observing within the original 25-second cleanup bound;
it did not change production limits, hooks or trace duration. The failed run and its
diagnosis remain retained alongside the successful run.

## Restoration and limits

The finalizer and a separate post-run check verified the original trace registration
spec restored and Available, original services healthy and idle, test namespaces
and grants absent, added baseline seccomp removed, and local private TLS/token/audit
files removed. Every captured test-owned BPF link/map/programme was absent. Existing
incident seccomp and observer tools were preserved; verified image caches remain
available for subsequent local tests.

The supported rollback procedure for this window is a preflight-checked
`helm upgrade` of the previous verified package and matching values. Native
`helm rollback` is not claimed because the older chart has no pre-rollback hooks.
No published beta predecessor is invented from these development sets.

Hosted [run 36757090991](https://github.com/danushkastanley/KubeMemLens/actions/runs/36757090991)
subsequently passed ARM reproduction and separate amd64 clean-consumer verification
on PR161's synthetic merge revision `0f76e52bf14bbe74c3e4f5f8c525f34ec9e7d7dc`.
Its parents were base `9258b581ada2b6921e6d76d02a12fd8321df8800` and tested head
`c9924cecc9458104977d0e7a899ccd12e60d5551`. PR161 merged after all 13 checks passed.
The hosted consumer performed no installation and retained development signing
authority. Resource qualification and final EKS execution remain separate gates.
These results grant no supported provider profile, publication approval or
public-beta readiness. See [the lifecycle procedure](RELEASE_LIFECYCLE.md).
