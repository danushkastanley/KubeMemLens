# Optional trace release build contract

Status: BPFB-002 implementation in progress. A local consumer invocation verifies
real signatures, metadata, file inventories, all four payloads and packaged Helm
rendering. Fresh local workers, launchers, BPF objects and the two-platform image
also reproduce and pass the consumer. Local active upgrade, guarded rollback, disable and uninstall also pass, including
standard-product coexistence and restoration. The read-only CI workflow is authored
and linted; hosted execution and merge remain outstanding. This document does not qualify or publish a trace release.

See [ADR 0022](../adr/0022-verify-optional-trace-release-as-one-bundle.md) for the
trust boundary and lifecycle decision.

## v1

Build type:
`https://github.com/danushkastanley/KubeMemLens/blob/main/docs/ebpf/RELEASE_BUILD.md#v1`.
This contract describes an optional bundle; the standard chart and release stay
separate. No command may infer installation acceptance from successful packaging.

A canonical `trace-release.json` is the sole subject inventory. Its explicit schema
version is 1; it binds a source commit, development or beta version, trace contract
1, and one immutable image reference under
`ghcr.io/danushkastanley/kube-memlens-trace`. It lists exactly these payload roles:

- `chart`: the optional Helm chart package;
- `engine`: launcher/worker binaries and public engine verification material;
- `image`: the OCI archive containing both Linux architectures;
- `programmes`: signed file/cache/OOM programme material and retained sources.

Each payload has a SHA-256 digest, byte count, provenance descriptor and SBOM
inventory. Images require separate Linux amd64 and arm64 SBOMs; the other roles
have an architecture-independent bundle inventory. Each descriptor names one
bounded regular file at the bundle root. Unknown roles, duplicate names, path
components, symlinks, missing content and extra bundle members fail closed.

`release-subjects.txt` is generated from the manifest's payload, SBOM and provenance
descriptors. It is never a second independently maintained inventory. The complete
asset set adds the manifest, its Sigstore bundle and the derived subject list.
Signatures and the manifest cannot recursively list their own hashes.

## Trust and consumer staging

The consumer copies a complete, hash-matched set into a new private directory
before calling verification tools. Source files remain untouched; an existing
destination is never replaced. The manifest is written last. A failed or partial
copy is unverified. Copying bytes is not signature verification.

The verifier executable and trust material are supplied by the caller and checked
against independent SHA-256 pins. Neither a bundled public key nor a filename can
choose the verifier. The Cosign process receives owned copies of the manifest,
signature and trust inputs, a bounded timeout and an environment without inherited
registry/cloud credentials or Sigstore overrides. Candidate executables are never
run during this authentication step.

Local development verification uses an explicitly pinned public key and accepts
only `-dev.N` manifests. Local signing uses an explicit Sigstore signing config
with no Fulcio, OIDC, Rekor or timestamp services. Its verifier permits the absence
of transparency evidence only in that local mode; the result is labelled
`local-development` and cannot authorise a beta release.

The beta verification path requires a separately pinned Sigstore trusted root,
expected version and source commit, the GitHub Actions issuer
`https://token.actions.githubusercontent.com`, and this exact certificate identity:

```text
https://github.com/danushkastanley/KubeMemLens/.github/workflows/trace-release.yml@refs/tags/v<VERSION>
```

It does not use regular-expression identities or disable certificate/transparency
checks. This is a verification policy for the forthcoming protected workflow, not
evidence that such a signed release currently exists. Existing tag, documentation,
release-environment and publication approval gates still apply.

## Provenance and SBOM relationships

Provenance uses the [SLSA v1 predicate](https://slsa.dev/spec/v1.2/build-provenance)
in an in-toto Statement v1. Each statement names exactly its payload and SHA-256.
Its project-specific `externalParameters` are exact: source URI, source commit,
release version, trace contract and payload role. The resolved source dependency
must match the same commit; other materials need immutable digests. The builder
identity comes from the consumer's trust policy. Timing must be well formed and
ordered. Unknown standard envelope fields remain allowed, while unknown project
input parameters fail closed. The format alone establishes no SLSA level.

[SPDX 2.3](https://spdx.github.io/spdx-spec/v2.3/package-information/) documents bind
the described primary package to the exact payload filename, release version and
SHA-256. A project-specific `OTHER` external reference with type
`https://github.com/danushkastanley/KubeMemLens/trace-platform` identifies the
manifest's required platform. Package identities are unique. A matching wrapper
is necessary but not sufficient: complete dependency scanning and agreement with
the actual OCI, engine, programme and chart contents remain separate checks.

Metadata parsers reject ambiguous JSON, private runner paths/key material, excessive
structure and unsafe resource references. Signing authenticates the submitted
claims; it does not independently prove that a scanner found every dependency or
that a declared build was reproduced. Retain real build/scan transcripts and
verify package relationships before installation.

See [Sigstore verification](https://docs.sigstore.dev/cosign/verifying/verify/),
[installation acceptance](WORKER_INSTALLATION.md) and
[compatibility](TRACE_COMPATIBILITY.md). No signing or provenance result grants
runtime policy acceptance, provider support or resource qualification.


## Archive reader boundary

`prototype/trace/releasebundle.ReadArchive` uses the Go standard tar/gzip readers
without extraction. Callers choose raw tar or single-member gzip explicitly and
supply bounded input size, expanded size, member size and entry count. Only regular
files and directories are accepted. Paths must be canonical and bounded; duplicate
names, conflicting file/directory trees, links, device/FIFO entries, extended
attributes and set-id or group/world-writable modes fail. Archive ownership fields
must be empty/root with UID/GID zero to avoid publishing local owner metadata.

File contents are streamed into SHA-256. Only named small metadata files are
retained, with a combined 32 MiB ceiling and owned-copy access. Truncation, bad gzip
checksums, concatenated gzip members and non-zero content after the tar terminator
are rejected. Zero padding counts towards the expanded-byte limit. These checks
establish an archive inventory, not its signature authority or agreement with the
release's image, chart, engine and programme contracts.

## OCI image checks

The trace image profile uses a plain OCI layout containing one image index, with
exactly one Linux amd64 and one Linux arm64 manifest. Every blob is local,
SHA-256-addressed and referenced; URL descriptors, embedded blobs, orphan blobs
and inline attestations are outside this profile. The publisher's external
manifest/signature binds the final OCI index. No tag lookup is used by inspection.

Inspection checks the input archive digest/size, descriptor sizes/digests, platform
configuration, layer compressed digests and the corresponding uncompressed diff
IDs. Layer data is bounded cumulatively per platform, including tar headers and
padding. Content is inspected without extraction. The scratch-image file tree is
restricted to the launcher and the declared worker/reference/programme/verification,
licence and source directories. Whiteouts, conflicting paths and file replacement
across layers fail; repeated directory entries must retain their modes.

The image must default to UID/GID 65532, the absolute `/memlens-trace` entrypoint and
`doctor --json`. It may have no environment or the exact standard Dockerfile PATH;
other environment settings, volumes, ports and altered commands fail. The legacy
`ArgsEscaped` field is Windows compatibility metadata and does not change this
Linux-only argv check. Source/version/contract labels and creation time must agree,
and filesystem history must account for every layer. Required worker and public
verification files must be present. A subsequent bundle check still needs to match
their exact bytes and signatures, including executable and programme architecture.

Local evidence includes a real two-architecture image assembled from current
launchers and retained signed worker inputs. Its 510 entries per platform passed
inspection, and launcher/worker hashes matched the recorded inputs. Changing a
real layer was rejected even when the test supplied a newly calculated outer
archive hash. This exercised the OCI boundary, not a complete release build,
installation, signature chain or resource/provider qualification.

## Engine and programme agreement

Engine and programme payloads use plain tar so the verifier can perform bounded
random-access rechecks without extraction. The engine contains two fixed
architecture directories, `linux-amd64` and `linux-arm64`, plus public verification,
reference, licence and source material. Each architecture supplies `memlens-trace`
and `memlens-filecache-worker`. Private keys, an installation acceptance policy and
additional executable names are outside the engine layout.

The engine's existing Ed25519 signature must verify over the canonical engine
release. It binds both worker digests to the reviewed SDK source and patch. The
retained patch must match. Executables are copied into bounded owned byte buffers
before ELF and Go build-information inspection; their class, byte order, Linux
architecture, static linkage, command identity and compiler settings are checked.
Candidate binaries are never executed. This supplements the runtime's independent
acceptance and sealed-executable checks; it does not replace them.

The programme payload must contain all six file/cache/OOM and architecture pairs.
The candidate index remains explicitly `unapproved`. Each signed review manifest
must match its index entry, kind, architecture, source/build record, reviewed
compiler/SDK/patch and exact object/OCI digests. Objects pass the existing userspace
ELF/BTF and programme-ABI validator. The fixed OCI artefact shape, empty config,
source files and complete blob inventory must agree; undeclared content fails.

Build records preserve their original bytes and `loaded: false`/no-approval meaning.
Their source and object hashes are checked; the recorded compiler headers must
match the engine's retained headers. A `reproduced` declaration is not a substitute
for retaining and checking the actual two-build transcript and outputs.

Finally, every image's engine/programme file copy must match the corresponding
archive's size, mode and digest for that architecture. Extra or missing copies
fail. Inner engine/programme keys establish relationships only after the outer
release manifest has been authenticated against caller-owned trust. The verifier
never turns a candidate index into a runtime acceptance allowlist.


## Chart and payload consumer inspection

The chart package must contain only the optional chart's metadata, values, schema,
README and flat templates. Its version and app version match the manifest; its
image defaults pin the exact OCI index under the trace image repository. It remains
disabled with no administrator policy, node identities or trust material supplied.
Unknown metadata, duplicate YAML mappings/documents, subcharts, executable members,
remote schema references and changed default permissions fail inspection.

`inspect-trace-release` accepts a bounded request derived from the authenticated
manifest and a private snapshot directory. It opens only the four named regular
payload files, checks their sizes and digests, inspects their contents and requires
agreement between image, engine, programmes and chart. It does not extract or run
candidate executables, contact Kubernetes, authenticate the outer release signature
or grant installation acceptance. Its receipt explicitly records
`authenticated: false` and `runtimeExecuted: false`. The orchestrating consumer
must establish those separate trust and qualification boundaries honestly.

The repository's chart contract check also accepts a packaged chart and its exact
expected image reference. For a package it leaves the image values unset, exercising
the packaged defaults. Helm lint, disabled/enabled rendering, rejected configuration
cases, reserved namespaces and numeric names are checked. Every API, node and
preflight container must use the exact expected image. The rendered resource kind,
API version, namespace, identity uniqueness and count must match the contract;
unknown workloads and additional grants cannot hide outside per-resource checks.
Rendered YAML is bounded to 4 MiB and 64 documents. This fixed two-node fixture is
an offline packaging check, not an installation or provider qualification.

Local package evidence includes all four real payloads, twelve image substitutions
across six rendered workloads, hidden/duplicate/wrong-namespace resources, and four
repacked chart mutations with recalculated outer hashes. Metadata inspection rejects
changed defaults or chart versions; Helm rendering also rejects a hardcoded image
inside a template. The local signature/SBOM orchestration described below now passes; fresh local
production and lifecycle checks now pass. Hosted workflow execution remains outstanding.


## Consumer command and SBOM file agreement

Build `prototype/trace/cmd/inspect-trace-release` and
`hack/trace-chart-contract` from independently trusted source. The consumer entry
point is `python3 hack/trace-release/verify.py --help`. It requires the bundle,
a new snapshot destination, expected version/source commit, explicit local-key or
GitHub-root trust, and absolute paths plus independent SHA-256 pins for Cosign,
the inspector, Helm and the chart checker. None of these tools or trust roots may
be selected from the candidate bundle.

The consumer copies each pinned tool into its own private temporary directory,
authenticates the staged manifest, verifies every provenance/SBOM relationship,
inspects all payloads, compares SBOM file inventories with the inspected bytes,
and checks the packaged chart through Helm and the rendered contract. It rechecks
the complete snapshot and the authenticated signature bytes before returning a
receipt. Linux and macOS tool invocations have fresh local configuration, no
inherited registry/cloud credentials, explicit timeouts and bounded output files.
A failed invocation returns no success receipt and leaves its snapshot unverified.

The chart fixture generator is shared by the repository check and consumer.
The standard chart separation check remains in the repository check; an offline
consumer receipt does not claim to have tested an existing standard installation.
No consumer command installs resources, starts candidate code, approves a runtime
policy, publishes artefacts or performs a vulnerability/licence audit.

`sbom.py` retains Syft 1.49.0 package and checksum records and adds a distinct outer
SPDX package for the downloadable archive. In particular, the image scanner's
configuration digest remains attached to its original package; it is not silently
replaced with the archive hash. The added package binds the payload hash/version
and platform. Its file relationships account for every regular file observed by
the non-extracting inspector. Missing, extra, duplicated, changed or unowned file
records fail. Adding file records does not invent package discoveries or licence
conclusions: unanalysed licence fields remain `NOASSERTION`.

Local integration scanned both image architectures (168 package records each),
the engine (335 records), the programme archive and chart. The final SBOMs add one
outer package each. Consumer verification matched 13 chart files, 263 engine files,
297 files in each platform image and 36 programme files. Seven real consumer cases
passed, including valid verification before/after probes and rejection of a wrong
key, changed subjects, damaged signature, and freshly re-signed but inconsistent
SBOM/provenance data. The local signing keys were removed after the run.

Those receipts have `local-development` authority and explicitly say installation,
fresh build reproduction and resource qualification were not performed. The
integration bundle records assembly from retained verified payloads; it is not
fresh-build or GitHub release provenance. File inventory agreement does not prove
that a package scanner can discover every possible ecosystem dependency.


## Local producer steps

The build-only worker command remains
`prototype/trace/worker/build_worker.py`. `hack/trace-release/build_launchers.py`
uses the same pinned compiler image, read-only source/module cache and disabled
networking. Each Linux architecture is built twice and compared. Licence discovery
uses a native helper with the target architecture passed to its package graph;
notices for both architectures are retained. Disposable compiler caches are cleared
between stages to stay within the fixed temporary filesystem budget.

`prototype/trace/programmes/build_filecache.py --set all` reproduces all six BPF
objects without loading them. Existing programme signatures may be retained only
when the fresh build record and both object copies match the signed bundle exactly.
The `sign-trace-engine` command separately checks both worker executable formats
and independently supplied hashes before signing an engine declaration with a
caller-owned private key. It writes only public verification material and cannot
create runtime acceptance.

`assemble.py` checks source/build records and reproduction outputs, collects the
explicit public licence/source/reference roots, and writes deterministic plain-tar
engine/programme packages. `build_image.py` uses the fixed scratch/COPY recipe and
exports both platforms twice with a fixed source epoch. `package_chart.py` reuses
the standard product's deterministic chart packager, preserving disabled defaults
and inserting the exact version/image identity. Downstream inspection remains
mandatory; producing an archive is not approval to execute it.

`finalise.py` requires independently pinned inspector and Syft executables. It
inspects the payloads, scans all required platforms, binds complete file inventories,
and creates local provenance plus the one canonical manifest and derived subjects.
The provenance retains bounded public build evidence and identifies its operation
as local scan/finalisation. Tool-source fingerprints must match that evidence.
Signing is a separate caller action, followed by `verify.py`; neither step grants
installation authority. Syft has a 128 MiB per-file scratch ceiling for inspected
members, while its reports remain limited to 16 MiB.

A fresh local development set passed both repeated workers, both repeated launchers,
six repeated BPF objects, and two byte-identical OCI archive exports. Its signed
consumer matched 13 chart files, 485 engine files, 519 files per image architecture
and 36 programme files. The consumer receipt correctly leaves its own reproduction
claim false: the separate producer receipts establish reproduction, while the
consumer authenticates and inspects the delivered set without rebuilding it.
The local lifecycle checks are recorded in [release verification](RELEASE_LOCAL_VERIFICATION.md).
Hosted workflow execution remains outstanding; publication stays separately gated.


## Read-only CI dry run

`.github/workflows/trace-release.yml` builds a development bundle on a standard
Linux arm64 runner, then verifies it on a separate Linux amd64 runner. The native
ARM builder matches the reviewed BPF compiler platform; see
[GitHub runner labels](https://docs.github.com/en/actions/reference/runners/github-hosted-runners).
Actions and build tools retain the repository's immutable pins. Both jobs have
only `contents: read`; the workflow has no OIDC, package-write, publishing or
installation job. Existing release-environment controls are unchanged.

`dry_run.py` requires a clean committed checkout and a development version, runs
the reproduced worker/launcher/programme/image pipeline, finalises the inventory
and signs with a new ephemeral local key. Private keys are removed in cleanup.
Only the explicit public bundle, public key and build summary are uploaded. The
consumer receives expected key/manifest hashes through job outputs, rebuilds its
verifiers from the exact source commit, authenticates the bundle and checks it
before any installation. Its result remains `local-development`, not GitHub release
provenance. Signature receipts also retain the selected external trust-root hash.
The workflow has passed local actionlint; a hosted run remains required.
