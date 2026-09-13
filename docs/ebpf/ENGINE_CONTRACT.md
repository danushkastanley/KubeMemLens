# Prototype engine and supply-chain contract

Date: 13 September 2026  
Status: accepted for staged prototype evaluation; no runtime programme approved

## Selection boundary

Inspektor Gadget remains the candidate engine behind KubeMemLens admission.
The evaluated source is [v0.56.0, commit
e5a2855f270ca6557f4bd7e4fabaddf6760d8f50](https://github.com/inspektor-gadget/inspektor-gadget/tree/e5a2855f270ca6557f4bd7e4fabaddf6760d8f50).
The original v0.54.1 set in [ADR 0003](../adr/0003-evaluate-inspektor-gadget-behind-kubememlens-admission.md)
is historical evaluation input, not permission to execute those programmes.

The following v0.56.0 OCI index identities were inspected and their signatures
verified with the release public key, together with their Linux amd64 and arm64
platform manifests:

| Artefact under `ghcr.io/inspektor-gadget/` | Index digest |
| --- | --- |
| `inspektor-gadget` | `sha256:bdb8f3ee121d94736570f6554b07d86022d887db9d6e9b9da20f7db31e32a7a1` |
| `gadget/trace_open` | `sha256:af88c9222e9c880251ab2093de9bdc13929f62b3cd9900c1c5c7b0df3304b2fb` |
| `gadget/top_file` | `sha256:c0eb11996a0ebe92bcaacf4d05767d6bded1161b1ea3188d1e093e9360165592` |
| `gadget/trace_oomkill` | `sha256:733b90f011f7bea7f72ae9a954d49a31a94334832b1862f969d2eda2c9d62f0d` |

The candidate builder index is
`ghcr.io/inspektor-gadget/gadget-builder@sha256:d38c1d47e975cdcfe2354555c49ae51365e40d49111240ac54530ba9fdb0ad6d`;
its index and arm64 platform signatures were verified separately. The arm64
builder platform is
`sha256:d55d33bfd2583e721ac78972a8039fb4b207d157e06f0c623c5f8a55cee597b4`.
Rebuilding all three gadgets for both target architectures from the pinned
source with this builder produced byte-for-byte matches with all six published
BPF objects. Verified signatures and reproduction establish signed identity and
object reproducibility, not compliance with the runtime contract.

No row in this table is an execution allowlist. The unmodified gadget set fails
the required cgroup filtering and evidence semantics described below. A custom
programme requires its own frozen source, licence review, object and OCI digests,
signature verification and build record before its first load.

## Source findings

These findings are tied to the evaluated commit. They are source inspection,
not a live-kernel security or performance qualification.

| Finding | Pinned source | Required response |
| --- | --- | --- |
| The upstream chart grants SYS_ADMIN by default and mounts broad host directories, including writable host configuration paths | [DaemonSet](https://github.com/inspektor-gadget/inspektor-gadget/blob/e5a2855f270ca6557f4bd7e4fabaddf6760d8f50/charts/gadget/templates/daemonset.yaml) | Do not install or copy this profile; demonstrate a separate minimal integration |
| Common target filtering uses a mount-namespace map and is disabled by default | [Filter](https://github.com/inspektor-gadget/inspektor-gadget/blob/e5a2855f270ca6557f4bd7e4fabaddf6760d8f50/include/gadget/filter.h), [namespace filter](https://github.com/inspektor-gadget/inspektor-gadget/blob/e5a2855f270ca6557f4bd7e4fabaddf6760d8f50/include/gadget/mntns_filter.h) | Configure exact admitted cgroup filtering before attach, with a deny-all unset state |
| File aggregation counts requested bytes at VFS entry and retains file paths; it does not observe cache hits/misses or completed I/O | [top_file](https://github.com/inspektor-gadget/inspektor-gadget/blob/e5a2855f270ca6557f4bd7e4fabaddf6760d8f50/gadgets/top_file/program.bpf.c) | Keep requested/completed I/O separate and add reviewed cache hooks for actual cache evidence |
| OOM output filters the victim's mount namespace but includes the invoking task's process context | [trace_oomkill](https://github.com/inspektor-gadget/inspektor-gadget/blob/e5a2855f270ca6557f4bd7e4fabaddf6760d8f50/gadgets/trace_oomkill/program.bpf.c) | Construct victim-only output inside the kernel; downstream redaction does not repair pre-emission isolation |
| Failed startup and successful shutdown follow different cleanup paths | [Runner](https://github.com/inspektor-gadget/inspektor-gadget/blob/e5a2855f270ca6557f4bd7e4fabaddf6760d8f50/pkg/gadget-context/run.go), [OCI lifecycle](https://github.com/inspektor-gadget/inspektor-gadget/blob/e5a2855f270ca6557f4bd7e4fabaddf6760d8f50/pkg/operators/oci-handler/oci.go), [eBPF lifecycle](https://github.com/inspektor-gadget/inspektor-gadget/blob/e5a2855f270ca6557f4bd7e4fabaddf6760d8f50/pkg/operators/ebpf/ebpf.go) | Test partial attach separately; do not infer complete resource cleanup from a successful Run return or Close call |
| Ring loss accounting periodically reads/resets counters and reports through later event reads | [Tracer reader](https://github.com/inspektor-gadget/inspektor-gadget/blob/e5a2855f270ca6557f4bd7e4fabaddf6760d8f50/pkg/operators/ebpf/tracer.go) | Establish cumulative accounting including concurrent increments and final unread loss |

The unmodified SDK lifecycle tests passed in a Linux container without BPF
privileges. They confirm the generic operator call sequence, including failed
startup. They do not reproduce a kernel attachment leak or prove kernel teardown.

## KubeMemLens-owned interface

[`internal/trace`](../../internal/trace/engine.go) owns the engine interface,
versioned internal results and fixed file/cache/OOM observation vocabulary.
It imports no upstream engine types and loads no BPF code.

A specification contains a fixed trace kind, server-resolved target, immutable
path policy and bounded limits. It cannot carry a gadget reference, bytecode,
kernel symbol, attach point or arbitrary parameter dictionary. Syntax validation
requires a full containerd identifier, Pod and Node UIDs, container start time
and cgroup identity. This validation does not authenticate a caller or prove the
kernel binding; admission and the live node resolver must do that.

Ordinary formatting and JSON encoding of internal targets, specifications and
events cannot retain their sensitive content. A transport must deliberately
encode an authorised ephemeral representation. Path and command values require
explicit access, while terminal escaping, consent checks, exact process-name
limits and total encoded-byte accounting belong to the session/output boundary.

File operations, cache additions/removals and OOM decisions are different types.
Requested and completed file bytes remain distinct. Cache activity in a selected
task's context does not establish ownership of shared pages. OOM scope may be
unknown. Unknown event counts remain nil rather than being reported as zero.

The contract is internal version 1. Public stream framing and API negotiation
are not introduced by this change. Standard agent snapshots and incident schemas
remain unchanged.

## Constrained integration under evaluation

Evaluate a separately built Linux worker with one engine session per process,
supervised by the optional node component. The control service and standard
agent must not import the BPF loader. A worker must not accept general upstream
CLI arguments or a remote gadget reference. Its fixed programme bytes and
metadata are verified before the worker can attach.

Use only explicit SDK operators. Exclude runtime socket discovery, container
hooks, symbolisation, networking, GPU, remote exporters and generic gadget APIs.
Do not invoke startup paths that auto-mount host filesystems. The public SDK
operator interfaces permit a constrained adapter to be evaluated without first
assuming an upstream fork is necessary.

The supervisor must enforce expiry, bounded worker output and termination after
parent loss or partial startup. Worker process exit may close descriptors, but
kernel enumeration is still required to prove there are no owned residual links,
programmes, maps or pins. A wrapper must preserve errors and cannot silently
convert cleanup uncertainty into success.

The proposed privilege budget is BPF/PERFMON only where demonstrated necessary,
read-only cgroup/kernel metadata, and no runtime socket, SYS_ADMIN, privileged
mode or writable host filesystems. This budget has not yet passed node preflight
or verifier testing. Failure to implement within the accepted security profile
blocks progression; it does not authorise a broader profile.

## Supply-chain and redistribution gates

The release checksum file passed upstream signature/bundle verification. The
downloaded deployment manifest and CLI SBOM matched those verified checksums.
Gadget configs and six platform BPF objects matched their OCI content digests and
were inspected without loading. Engine BuildKit provenance describes the
evaluated source revision for both architectures. The six BPF objects were
reproduced exactly using the upstream Clang/strip pipeline, in a network-disabled
container with all capabilities dropped and read-only source. Full engine image,
builder image and timestamped OCI index reproduction were not performed.

The published CLI SBOM lists 188 components without licence fields. It cannot
close the licence gate. Resolving the proposed Linux SDK imports identified 151
Go modules and 1,279 packages including the standard library. The
[package licence inventory](engine-dependency-licences.csv) has 170 records:
86 Apache-2.0, 42 MIT, 37 BSD-3-Clause, two BSD-2-Clause, two MPL-2.0 and one ISC.
The generated `github.com/in-toto/attestation/go/v1` package was resolved manually
to its module's Apache-2.0 licence. Non-Go assembly, embedded BPF and C headers
require the separate source inventory; the Go scan does not establish their
complete dependencies or obligations.

The source includes Apache-2.0 Go code, GPL-2.0 file/OOM programmes and separately
licensed BPF helpers. The top-file source offers LGPL-2.1 or BSD-2-Clause terms;
its BPF programme licence string is not a substitute for source licence review.
Dependency review must also account for MPL-2.0 components and dual-licence
choices. Record the chosen terms and preserve required copyright, licence,
notice, source and modification information for each redistributed component.
Do not label the complete optional artefact Apache-2.0 merely because that is the
standard repository licence.

| Licence family in the candidate | Distribution record required |
| --- | --- |
| Apache-2.0 | Retain licence and applicable notices; record modified files and preserve attribution |
| MIT, ISC, BSD-2-Clause, BSD-3-Clause | Retain the relevant copyright, permission/conditions and disclaimer text; preserve the BSD non-endorsement condition where present |
| MPL-2.0 | Identify covered files and modifications; retain notices and provide the required covered-source availability with distribution |
| GPL-2.0 BPF source/objects | Keep the programme's licence separate; provide corresponding source and build material through a reviewed distribution mechanism |
| Dual-licensed helpers | Record the selected licence alternative per file and retain its terms; do not infer the choice from the ELF programme licence string |

This inventory is for the evaluated import set. Re-run it for the actual optional
worker and its generated objects before distribution. No upstream code or binary
is redistributed by the current internal contract files.

The candidate module contains replacement directives. A separate SDK module
must review and explicitly preserve any applicable replacements; downstream Go
modules do not inherit them automatically. Do not introduce these dependencies
or replacements into the standard application's module as a side effect.

The [SDK dependency review](ENGINE_DEPENDENCIES.md) records the vulnerability
scan, including three containerd findings whose coarse Go records differ from
the primary advisory ranges. The actual worker graph and applicability decisions
must be reviewed before runtime acceptance; signed or reproducible inputs are
not a vulnerability-free claim.

Before accepting a runtime candidate, complete:

1. The final worker import graph, complete licence obligations and removal path.
2. A source-to-object reproduction record for each custom programme using the
   pinned builder/platform, with any non-reproducible boundary recorded precisely.
3. Reviewed custom programme fields, filters, hooks, counters and resource layout.
4. Final programme/metadata digests and signatures matched to the engine version.
5. A fail-closed runtime allowlist and ownership-specific revocation policy.
6. Fresh maintainer acceptance of the design and privilege inventory.

The project maintainer, `danushkastanley`, owns candidate selection, dependency
refresh and digest revocation. A release must name the responsible human in its acceptance record.
Reject unknown versions, mismatched child manifests, modified objects, unsigned
replacements and revoked digests. Never refresh a tag at runtime. An emergency
disable stops new admission, cancels affected active sessions and requires
teardown evidence before optional resources are removed.

No optional image, chart, runtime, provider claim or release is distributed by
this contract. Removing these unused internal contracts has no stored-data
migration effect. Independent security and benchmark gates remain unchanged.

## Maintainer decision, 13 September 2026

The maintainer accepted the constrained v0.56.0 SDK candidate and its requirement
for custom reviewed file/cache and victim-only OOM programmes. This decision does
not approve the original three gadget binaries for execution or broaden the
privilege profile.

The original ticket order freezes the engine before programme implementation.
The accepted staged freeze records the exact upstream engine, builder and
reference objects now; permits non-attaching preflight, admission and in-memory
session work next; and requires a second source/digest/signature acceptance for
each custom programme before the file/cache or OOM prototype can load it.
Reference gadget digests are not approval for modified programmes. Independent
security review and benchmark acceptance remain required for progression to R7.
