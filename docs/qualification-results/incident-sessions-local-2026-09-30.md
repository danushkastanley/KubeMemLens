# Incident sessions: local functional qualification

Date: 30 September 2026. These are maintainer-run local checks, not independent
review, resource qualification, managed-provider evidence or a release approval.

The opt-in incident API, CLI and TUI were exercised on a two-node kind cluster:
Kubernetes 1.37.0, LinuxKit 7.0.12, containerd 2.3.4 and arm64. The standard image
was a local development build; its source and image identities are recorded in
the [machine-readable result](incident-sessions-local-2026-09-30.json).

## Verified behaviour

- Real Kubernetes ServiceAccount authentication and delegated permissions;
  owner and namespace isolation; trace-subresource revocation while status and
  authorised export continued to work. Denied attachment left the record intact.
- Explicit creation, masked annotations, real Pod capture, retained comparison,
  local agent history and Kubernetes Event markers, trace references, closure
  and deletion. Disabled marker acquisition remained a visible gap.
- Sanitised exports omitted private annotations, identities, digests and capture
  bytes. Authorised exports retained the evidence. Trace requests carried only
  typed references, with report bodies remaining local. File verification matched
  original bytes and rejected changed whitespace without asserting authenticity.
- A real one-hour session expired with the same collector process throughout.
  There were 114 successful pre-expiry observations. After expiry, status and
  full export returned the precise missing-session result; the same credentials
  could still create, export and delete a new session. Token expiry, restart and
  transient transport failure were not accepted as retention evidence.
- Installation of the signed published RC1 chart/image, followed by upgrade to
  this development build, returned fresh data for the unchanged fixture Pod.
  The current CLI replayed actual RC1 Pod/history captures without rewriting
  their bytes. New non-trace timelines used schema 1; trace references selected
  schema 2 and passed offline verification after the upgrade.
- Current readers also replayed two real schema-1 timeline exports from the
  earlier development implementation. RC1 has no incident-session feature:
  this is not a claim of prior released session support or persistent recovery.
- Actual terminal journeys at 80×24 and 160×35 covered creation, masked notes,
  capture, mode-0600 exports, closure, confirmed deletion, navigation and process
  exit. Trace-bearing views displayed operator provenance and the unverified
  source caveat without displaying private notes or report digests. Leaving the
  panel preserved the server record until explicit deletion, as documented.
- The test release, rendered chart objects, fixture namespaces and generated
  credentials were removed. The original separate tracing installation remained
  healthy. Private exports were retained under local evidence controls.

## Validation and limits

Root Go tests, race tests, vet, formatting, build and the vulnerability check
passed; the scanner reported no called vulnerability. Linux worker and prototype
race/vet checks passed, as did worker builds for arm64/amd64, all five standard
Linux amd64 commands and the Windows amd64 CLI. Cross-compilation does not prove
runtime behaviour on Windows or Linux amd64. Chart, source-binding, release,
terminal and provider contract checks passed; immutable-source checks used an
isolated exact-file Git snapshot because the implementation was uncommitted.

The public [walkthrough](../../hack/verify_incident_sessions.py) supports an
existing report via `--trace-report`. The live trace-reference cases used retained
real file/cache reports; they did not start another kernel trace. TUI report-save
and reference submission additionally have real TLS/file integration tests.
The optional worker, performance and provider qualification gates remain separate.

Earlier PTY harness attempts expected complete redraw strings or new output from
a no-op Home key. API reads confirmed the actual capture/close state, and the
successful harness also checked exports and backend state. Failed transcripts
were retained and their sessions removed. No product assertion or timeout was
relaxed. The initial full-check attempts also exposed an identity-helper dependency
regression, which was fixed before the successful Linux checks.

Session retention applies to collector memory, not exported files. There is no
database, live-session restoration, extra application-level RAM encryption or
multi-replica persistence guarantee. Deployment audit/proxy logging and final EKS
qualification still require their own checks.
