# Worker installation acceptance

Status: the normal-exit and path-copy candidate is accepted for bounded local
testing and installed with its matched immutable public policy. See
[local qualification](FILE_CACHE_LOCAL_QUALIFICATION.md) for workload evidence.
Full failure/lifecycle, independent review and provider gates remain incomplete.

`prototype/trace/workerinstall` parses bounded canonical installation policy,
supplied independently of operator requests and candidate bundles. Parsing
validates declarations; it does not grant maintainer approval to install them.

## Identity and signatures

Policy contains a signed engine release, engine verification key/signature,
programme verification key, accepted programme manifests and the common candidate
index digest. An engine release binds the pinned SDK source commit and patch to
one or two architecture-specific worker executable hashes. Its Ed25519 signature
must verify. Engine and programme trust keys are explicit and may be independent.

| Identity | Meaning |
| --- | --- |
| Engine digest | SHA-256 of canonical signed engine-release payload, common across supported architectures |
| Worker hash | Exact static Linux executable for one accepted architecture |
| Programme digest | SHA-256 of the candidate index containing all four file/cache review manifests |
| Accepted manifest hash | Independently permitted signed manifest for the requested kind and architecture |

The index must match the programme key and every independently accepted manifest.
Reading an index does not accept its other entries. A common stream identity binds
both architectures while the node verifies its specific executable and programme.
A changed worker requires a new engine signature and changes the engine digest.
A changed programme index changes the programme digest.

Admission's installation policy owns the expected engine digest. Node preparation
returns engine and programme identities, validated before activation and emitted
in metadata. The controller compares metadata against its frozen admission and
programme selection. The original upstream digest remains the default for the
staged baseline; a custom runtime must supply its installed engine identity.

## Target and executable ownership

Node `Runtime.Prepare` borrows the exact retained target handle. It must neither
close it nor duplicate a child descriptor before activation. The engine may
duplicate the handle during Run; the node owns it until confirmed teardown.
Pending-expiry, disconnect and adapter-panic quarantine rules remain in force.

On Linux, `Policy.Executable` copies the accepted executable into an owned memfd
while checking its hash, with a 128 MiB bound and fixed copying buffer. It verifies
ELF class/byte order/architecture and rejects interpreter/shared-library dependencies.
Write, growth, shrink, executable-permission and further-seal changes are sealed
before returning the descriptor. Path replacement or in-place writes to the
installation file cannot change it. Unsupported memfd/seal behaviour fails closed.

The policy itself is transferred in a sealed, non-executable descriptor, read
positionally so concurrent workers cannot race a shared seek offset. The child
checks that its inherited executable descriptor is the same file as
`/proc/self/exe`, sealed and hash-matched to its signed installation policy.

The dedicated worker entry point performs these checks, validates the private
request and retained bundle, and calls the SDK adapter. It accepts no CLI options.
The node launcher retains installation descriptors, duplicates the target only
during execution and passes fixed descriptor roles to the supervisor. It limits
active workers to two. Shutdown cancels and joins workers; uncertain cleanup
quarantines the runtime and cannot report successful shutdown.

Both `binding-node` and `admission-api` accept optional `--acceptance-policy` paths.
Omission keeps incident tracing disabled. Policy files must be absolute, bounded
regular files without group/other write permission. Contained projected-volume
symlinks are supported; escapes from the policy directory are rejected. The node
also has installation-only `--worker-executable` and `--programme-bundle` paths.
The API's `--allow-confirmed-paths` requires an accepted stream installation; it
does not replace per-request confirmation or authorisation.

The private stream request now carries the controller's expected engine and
programme digests. The node checks them before activation. Missing/old request
fields fail closed, so controller and node must be upgraded together for this
private contract change. The installed file/cache runtime selects public NDJSON
version 2 for default aggregate output. The private stream request also carries
the independently selected stream version; a mismatch is rejected before
activation. The controller pins the metadata version and uses the same version
for failure summaries. See [STREAM.md](STREAM.md) for the versioned contract.

The existing `binding-node.json` seccomp profile is baseline-only and lacks newly
required memfd, seccomp and BPF map operations. The separate candidate
[file/cache profile](FILE_CACHE_PROFILE.md) and matched image have capability-free
verification. An accepted policy is installed only in the isolated local test
cluster; the positive incident path remains unqualified.

The worker Dockerfile assembles verified node/worker binaries, the signed programme
bundle, public engine verification material and retained licences/sources. It
contains no installation policy or private key and defaults to `doctor --json`.
`kubernetes/render_filecache.py` renders a separate incident configuration that
references an independently installed policy ConfigMap; it does not create that
policy. Both services receive a read-only policy mount. Only the node receives
BPF/PERFMON and the incident seccomp profile; confirmed paths remain explicit.

## Evidence and limits

The [worker test harness](WORKER_TESTING.md) supports race-instrumented parents
with static child fixtures and explicit seccomp baseline-composition checks.

Race tests verify frozen identities, signed-release tamper rejection, architecture/
kind selection and candidate-index consistency. Node/controller tests verify exact
handle borrowing, selected metadata and invalid-identity rejection before activation.

Real Linux tests ran with all capabilities dropped, a read-only root and no network.
They executed a fixture from a sealed memfd, rejected writes and permission/size
changes, and proved source replacement could not change the retained executable.
Wrong hashes, architectures and final symlinks were rejected. These tests load no
incident BPF and do not qualify the unfinished worker's memory, privilege, seccomp,
parent-death or kernel teardown behaviour.
