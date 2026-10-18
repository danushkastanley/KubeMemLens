# Trace incident playbooks

These procedures concern the separate development trace extension. Preserve the
standard memory service and unrelated workloads. Trace results are ephemeral;
capture only the minimum authorised evidence, privately. Do not enable raw paths
or increase trace limits while diagnosing an incident.

## Common containment and evidence

Record the time, source commit, verified bundle digest and the failure category.
Keep installation values, object UIDs, certificates, admission IDs and logs in
restricted incident storage. Use an existing explicit [trace report](SUPPORT.md)
for a public support request after reviewing its remaining timings and counters.

For urgent containment, select the current verified chart and a reviewed values
file with `enabled: false`. Apply it using the explicit kubeconfig, context,
namespace and release form in [emergency disable](RELEASE_LIFECYCLE.md#emergency-disable).
Check that the trace registration and workloads disappear and new admissions are
refused. Continue observing recorded owned workers/BPF objects until cleanup is
confirmed; Helm may finish before process termination. Preserve an unknown result
when the observer cannot prove absence.

Do not delete arbitrary BPF objects, unpin unrelated maps, kill processes by name,
remove finalisers blindly or grant broader capabilities. Restore service only
after diagnosing the cause, resolving owned cleanup and rerunning preflight with
the exact approved configuration.

## Excessive overhead

1. Stop new admissions and cancel the exact active admissions where known. If
   control is unavailable or overhead continues, disable the extension.
2. Preserve the observation interval, active trace kind/bounds, paired standard
   baseline and process/resource measurements. A short diagnostic is not a
   complete benchmark. Preserve failed measurements as well as improvements.
3. Confirm owned-worker/BPF cleanup and that the standard memory API still serves
   fresh reads. Investigate idle and active costs separately using the frozen
   [benchmark protocol](BENCHMARK_PROTOCOL.md).

The current resource gate has failed; no supported trace profile can be inferred
from a successful installation. Do not raise thresholds, change workload timing,
add a scheduler override or shorten the measurement window to manufacture a pass.

## Stuck session or uncertain cleanup

1. Use `trace cancel ID` with the original namespace and authorised identity.
   Cancellation does not depend on successful contract negotiation or discovery.
2. Retain `confirmed` or `unconfirmed` exactly as returned. A terminal summary,
   missing ID or ended watch is distinct from a cleanup receipt. Unknown cleanup
   keeps its quota reservation; do not reset quota by restarting the controller.
3. Check the original node parent PID/start time, accepted worker hash and captured
   BPF IDs through the bounded ownership observer. If control remains unavailable,
   disable the extension and observe the same captured identities through teardown.

Deadline and watchdog bounds remain mandatory. A failure to observe within the
declared bound is an incident, not permission for unbounded retries. If ownership
cannot be established, preserve the evidence and escalate to the cluster operator;
do not substitute a host-wide cleanup command.

## Lost, truncated or missing evidence

Check the report's termination, incomplete flag, produced/delivered/lost/rejected
counts, transport completion and observation window. Unknown counters stay unknown.
An event or output limit is truncation; a missing summary cannot prove zero loss.
File/cache hook coverage and OOM attribution have separate limitations.

Keep the partial report and original limits. Do not interpret file opens as cache
hits, infer the OOM cause from a restart alone, or merge observations across a
replaced Pod/container. After cleanup is confirmed, an explicitly approved shorter
or quieter diagnostic may answer a narrower question; it does not replace the
failed qualification workload or turn missing evidence into complete evidence.

## Controller or node service unavailable

Check APIService availability, owned Pod readiness, node-profile drift, immutable
policy and certificate pins, certificate validity and network enforcement. Keep
raw events, Pod manifests and dependency errors private. Use only the workload's
required grants; do not bypass TLS, NetworkPolicy or admission policy.

Audit write failure, short write or queue saturation prevents new admissions and
fails readiness. Preserve the bounded audit evidence and repair the log delivery
path before a controlled restart. Node expiry and authorised cancellation remain
the containment mechanisms. See [audit behaviour](AUDIT_AND_RETENTION.md).

A controller replacement loses in-memory handles; it does not migrate or replay
results. Resolve old worker cleanup before restoring admissions. A node UID,
kernel/runtime or TLS change needs reviewed values and successful preflight.
If a new configuration fails preflight, inspect the actual running image and
policy; a failed Helm history entry is not proof that the target was installed.

## Suspected tenant isolation or privacy failure

Stop tracing and disable the extension. Preserve restricted evidence of the
authorised and denied identities, exact workload lifetimes, observed disclosure,
bundle/policy identity and cleanup outcome. Do not repeat the issue against another
tenant or collect extra raw traces merely to strengthen a report.

Use the private vulnerability process in [SECURITY.md](../../SECURITY.md).
Never post raw paths, arguments, tenant identifiers, tokens, certificates or a
suspected cross-tenant disclosure in a public issue. Preserve audit evidence with
appropriate access controls; keyed audit references are not anonymous data.
Resume tracing only after the defect and its regression evidence have been reviewed.
