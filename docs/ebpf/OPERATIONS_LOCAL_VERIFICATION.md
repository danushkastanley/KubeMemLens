# Local operations walkthrough

Own testing on 30 September 2026 UTC, using the
[operator guide](OPERATIONS.md). This was a fresh operator configuration and
isolated test installation on an existing development cluster, not an independent
administrator/adopter review or a managed-provider qualification.

The cluster used Kubernetes 1.37, LinuxKit 7.0.12 arm64, containerd 2.3.4 and
Cilium 1.20.1. The CLI source was main revision
`408c42a84456bf05a208f026eb300cf30a043c60`; this documentation change alters no
runtime code. The previously verified `0.1.0-dev.2` chart/image/policy set from
[release verification](RELEASE_LOCAL_VERIFICATION.md) was reused by immutable
digest. No new trace image, programme, privilege or limit was introduced.

## Commands and observations

The harness extracted and ran the guide's shell blocks with explicit administrator
and operator kubeconfigs. Separate fixture namespaces, ServiceAccounts, immutable
policy/TLS/audit prerequisites and a fresh private output directory were created.
The original trace registration was retained for restoration. Existing verification
helpers supplied the independent-of-the-CLI process/kernel observer; this is still
our own testing, not an external review.

Twenty-nine walkthrough assertions passed, covering:

- Packaged Helm installation and APIService availability, with metadata and host
  preflight hooks enabled and confirmed-path disclosure disabled.
- Real allowed and denied identities. An unbound identity could not start a
  session or produce a trace report.
- Public CLI preflight, a 15-second file trace and a 15-second cache trace, each
  capped at 1,000 observations. Bounded 8 MiB fixture I/O produced positive typed
  observations. Counts include runtime activity in the selected cgroup and are
  not presented as an exact attribution of every operation to the workload helper.
- Separate create/watch/cancel commands for a 30-second admission. The cancel
  command received a positive API cleanup receipt; the watcher preserved its
  own unconfirmed cleanup state after that admission was removed.
- Three schema-2 reports, 2,726, 2,566 and 1,556 bytes, written with mode `0600`.
  Fixture identities, tokens, runtime IDs, node names and endpoint values were
  absent. Reports retained observed versions, numeric evidence and actual outcomes.
- Active worker/link/map/programme witnesses for all three sessions, followed by
  absence of every captured owned BPF identity and an idle node process.
- Documented Helm uninstall, chart/hook removal, and fresh standard memory reads
  with unchanged standard controller/Pod/container identities after each trace
  and after removal.

The file/cache observations expired normally. Their CLI commands returned non-zero
with completed streams and `cleanup: unconfirmed`: the admission had disappeared
before the separate client cleanup request. The external observer proved actual
kernel cleanup; the reports were not rewritten to claim an API receipt. This
distinction is now explicit in the operations guide.

## Audit and restoration

The original local API server had auditing disabled. A temporary policy recorded
only metadata for the three tracing resources and no other API groups. All 16
recorded events omitted request and response objects. The exact original API
server manifest bytes were restored afterwards, and temporary host audit files
were removed. The retained audit records remain private.

The finalizer and a separate post-run check confirmed the original trace
registration restored and Available, original services healthy/idle, all test
namespaces/grants absent, the added baseline seccomp profile absent and local
private TLS/token/audit-key files removed. Existing incident seccomp, observer
tools and immutable image caches were preserved. Standard fixture resources were
removed after the fresh-read checks.

Two earlier attempts remain recorded: one stopped before test installation while
the existing APIService recovered from the audit restart; the next incorrectly
required a positive CLI cleanup receipt after expiry. Both completed restoration.
The successful run waited for APIService readiness and checked the documented
uncertainty alongside actual kernel absence. No product limits or safety checks
were relaxed. The active workload was added to avoid treating an idle fixture as
evidence of useful file/cache observations.

## Other checks and limits

Existing CLI, trace-report and incident tests passed. Local Markdown links, shell
syntax, support/community contracts and the diff whitespace check passed. This
walkthrough executed the documented commands locally; it is not a CI kernel run.
The preparation block's additional explicit-input guards were checked separately:
all 24 missing/empty-variable cases stopped before creating an output directory
or running a cluster command.
It induced no OOM and created no cloud infrastructure. Resource qualification,
managed-profile testing and release approval remain open. Independent reviewer
and adopter activity remains excluded, not passed.
