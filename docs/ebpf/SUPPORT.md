# Trace support and diagnostic sharing

The optional trace extension is development-only. Support is best effort within
maintainer capacity, with no response-time commitment, SLA or production stability
guarantee. Installing it does not extend the standard product's compatibility
claims. Independent reviewer and adopter evidence has not been obtained.

## Profile and evidence boundaries

| Profile | Current evidence and support state |
| --- | --- |
| Local kind, Kubernetes 1.37, LinuxKit 7.0.12 arm64, containerd 2.3.4, Cilium 1.20.1 | Our own functional [isolation](ISOLATION_LOCAL_VERIFICATION.md), [compatibility](COMPATIBILITY_LOCAL_VERIFICATION.md) and [release lifecycle](RELEASE_LOCAL_VERIFICATION.md) checks. Resource qualification remains open; not a production support claim. |
| Linux amd64 and arm64 release payloads | Both architectures built, inspected and reproduced; [release verification](RELEASE_BUILD.md) does not establish runtime or kernel support. |
| Managed Kubernetes, including EKS AL2023/containerd/amd64 | Final trace qualification has not run. No supported managed trace profile yet. |
| Other kernels, node images, architectures or runtimes | No support inferred from similar names or a successful baseline probe. Exact-profile qualification is required. |
| Autopilot, Fargate, virtual nodes, Windows, Kata and WebAssembly runtimes | Outside the trace support scope; use available standard memory evidence instead. |

The [operations walkthrough record](OPERATIONS_LOCAL_VERIFICATION.md) covers our
public CLI, diagnostic exports, metadata-only auditing and installation removal.

The chart accepts the explicit `development-linux-containerd` profile only after
an acknowledgement and exact node/policy/TLS preflight. This is a development
execution permission, not a support designation. A baseline `supported` check
means its particular prerequisite passed; it does not override the resource gate.
Refresh the evidence after relevant node image, kernel, runtime or artefact changes.

## What to share

For an ordinary support issue, start with the exact version/commit, trace kind,
fixed error category, a synthetic reproduction and the relevant runbook step.
Give only the operating-system/provider family and architecture needed to explain
the problem. Use the [project support route](../../SUPPORT.md); suspected security
failures go privately through [SECURITY.md](../../SECURITY.md).

The existing explicit `trace run`, `watch` or `export` output is the diagnostic
bundle for a session. Current writers use an allow-list rather than copying raw
frames or Kubernetes objects. The bounded JSON report contains:

- Schema/contract/tool versions, trace kind and frozen bounds.
- Engine/programme digests and report-local target aliases.
- State, fixed failure categories, cleanup state, timing, counters and caveats.
- Validated numeric summaries and uncertainty, where available.

It omits real namespace/workload names, Pod and container IDs, node identity,
admission handles, raw paths, arguments, event payloads and arbitrary error text.
The file is at most 32 KiB, written privately and never uploaded automatically.
Use `--export FILE --confirm-export` at the time of observation. A previous
completed stream cannot be exported later from the server.

Timings, digests and workload counters may still reveal operational information.
Review the report under your organisation's policy before sharing; use a synthetic
reproduction when those fields are sensitive. The `redacted` flag on an imported
file is not proof of its origin or privacy: archive validation preserves allowed
untrusted text. Do not reclassify arbitrary, edited or third-party JSON as a safe
report merely because it parses or carries that flag.

If preflight or connection setup fails before a session exists, a report may not
be written. Share only the fixed failure category and runbook step. Absence of a
report is not a successful run. Do not start a trace solely to obtain a support
attachment when access, ownership or prerequisites are uncertain.

## Keep private

Do not attach kubeconfigs, authentication material, private keys, TLS Secrets,
Helm values/releases, raw Kubernetes objects, preflight check values, screenshots
of named workloads, shell transcripts or raw node/API logs to public issues.
CLI stderr can include the selected workload and admission ID; it is not the
redacted report. Audit HMAC references still correlate identities and stay private.
Cloud account/project/subscription IDs and proprietary image names stay private.

There is no automatic support collector or upload. Keep authorised incident
evidence separately with restricted access and an explicit retention period.
Expiry and uninstall do not delete local exports or external logs. Follow the
[retention policy](AUDIT_AND_RETENTION.md) and preserve uncertainty about cleanup.
