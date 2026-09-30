# Operating the optional trace extension

The trace extension is an unqualified development feature. No production or
managed-provider trace profile is supported yet. Use an authorised development
cluster and an explicitly selected synthetic workload. The standard memory
installation stays separate and remains usable when tracing is disabled.

This guide joins the installation, client and removal procedures into one
operator workflow. For failures, use the [incident playbooks](INCIDENTS.md).
For information suitable for sharing, use [trace support](SUPPORT.md).
The [local walkthrough record](OPERATIONS_LOCAL_VERIFICATION.md) states which
commands and cleanup observations were exercised and what remains unqualified.

## Prepare the installation

Use a contract-1 CLI and extension from the
[compatibility window](TRACE_COMPATIBILITY.md). Install the extension before
updating clients. Tools required on the operator machine are `kubectl`, Helm,
the matching `kubectl-memlens` executable and the independently pinned bundle
verification tools described in [release verification](RELEASE_BUILD.md).

Before running Helm:

1. Verify the complete bundle and keep its receipt and private snapshot. Select
   the packaged chart from that snapshot and preserve its image digest. A local
   development signature does not authorise a beta release or installation.
2. Complete the [administrator-owned prerequisites](CHART_INSTALLATION.md):
   dedicated namespace, enforced NetworkPolicy, exact Node identities, reviewed
   seccomp profiles, independent immutable acceptance policy, TLS material,
   audit-reference key and narrowly scoped preflight ServiceAccount.
3. Apply the metadata-only Kubernetes audit rule from [the stream contract](STREAM.md)
   before using trace endpoints. Do not capture their request or response bodies.
4. Review enabled values against the actual nodes and policy. Keep private keys
   and tokens out of values. Record the UIDs of prerequisites that this installation
   owns; shared prerequisites must survive removal.
5. Inspect `v1alpha1.tracing.kubememlens.io`. Only one installation can own it.
   An existing registration requires an explicit migration and restoration plan;
   do not adopt or overwrite it during this walkthrough.

Use a shell session with the following values selected explicitly. File paths
must be absolute. The administrator kubeconfig installs the chart; the operator
kubeconfig belongs to a named identity with trace access in the target namespace.
They may use the same context name but need not be the same file or identity.

| Variable | Value to select |
| --- | --- |
| `trace_kubeconfig`, `trace_context` | Administrator kubeconfig and exact context |
| `trace_operator_kubeconfig`, `trace_operator_context` | Named trace operator's kubeconfig and context |
| `trace_namespace`, `trace_release` | Dedicated installation namespace and new Helm release name |
| `trace_chart`, `trace_values` | Verified packaged chart and reviewed enabled values file |
| `trace_target_namespace`, `trace_pod`, `trace_container` | Authorised synthetic target |
| `trace_output` | New private local directory for this walkthrough |

```sh
set -eu
: "${trace_kubeconfig:?Select the administrator kubeconfig}"
: "${trace_context:?Select the administrator context}"
: "${trace_operator_kubeconfig:?Select the operator kubeconfig}"
: "${trace_operator_context:?Select the operator context}"
: "${trace_namespace:?Select the installation namespace}"
: "${trace_release:?Select the new release name}"
: "${trace_chart:?Select the verified packaged chart}"
: "${trace_values:?Select the reviewed enabled values}"
: "${trace_target_namespace:?Select the authorised target namespace}"
: "${trace_pod:?Select the synthetic target Pod}"
: "${trace_container:?Select the target container}"
: "${trace_output:?Select a new private output directory}"
umask 077
mkdir "$trace_output"
kubectl --kubeconfig "$trace_kubeconfig" --context "$trace_context" \
  get apiservice v1alpha1.tracing.kubememlens.io
```

A NotFound response is expected for a new installation. Authentication or network
errors do not prove absence. Before proceeding, record a fresh standard memory
read of the fixture using your existing standard installation's public workflow.
Retain the selected Pod UID privately so the post-removal check uses the same
workload lifetime.

## Install and grant access

```sh
helm --kubeconfig "$trace_kubeconfig" --kube-context "$trace_context" \
  --namespace "$trace_namespace" install "$trace_release" "$trace_chart" \
  --values "$trace_values" --wait --timeout 5m

kubectl --kubeconfig "$trace_kubeconfig" --context "$trace_context" \
  wait apiservice/v1alpha1.tracing.kubememlens.io \
  --for=condition=Available --timeout=90s
```

Installation runs metadata/TLS/policy verification and one bounded host preflight
per node. An unsuccessful hook is a failed installation, even if earlier Jobs
succeeded. Diagnose it before retrying; never disable hooks or increase privileges
to get past a failure. Availability is a routing/readiness check, not evidence
that a particular caller or workload is authorised.

Bind the generated operator ClusterRole to the named operator using a RoleBinding
in the target namespace. The role name follows the rendered chart prefix from
[installation](CHART_INSTALLATION.md). The operator also needs the exact Pod read.
No operator grant is created by Helm. Verify both an allowed operator and a denied
identity through the real API; ordinary memory read access is insufficient.

## Preflight and collect a bounded report

```sh
kubectl memlens --kubeconfig "$trace_operator_kubeconfig" \
  --context "$trace_operator_context" trace preflight "$trace_pod" \
  -n "$trace_target_namespace" --container "$trace_container" \
  --kind files --duration 15s --max-events 1000 \
  > "$trace_output/preflight.private.json"

kubectl memlens --kubeconfig "$trace_operator_kubeconfig" \
  --context "$trace_operator_context" trace run "$trace_pod" \
  -n "$trace_target_namespace" --container "$trace_container" \
  --kind files --duration 15s --max-events 1000 \
  --export "$trace_output/files-report.json" --confirm-export
```

Preflight creates no admission or incident BPF attachment. Its baseline is a
startup observation with a capture time, not a new performance measurement.
`run` repeats authorisation and target checks, creates one admission and watches
its stream. The selected Pod UID and container lifetime cannot silently change.
File observations do not establish page-cache hits or misses. To investigate
cache additions/removals, repeat the commands with `--kind cache` and a different
report filename. OOM observation is separate; this walkthrough does not induce OOM.

Read transport completion, evidence completeness, loss, termination and cleanup
separately. Null counters are unknown, not zero. A completed file/cache stream can
still have limited hook coverage. A non-zero exit can leave a useful partial
report; retain its failure state. Existing output files are refused by default.

After normal expiry, the server may remove the admission before the client's
separate cancellation request receives a cleanup receipt. The CLI then reports
`state: completed` with `cleanup: unconfirmed` and exits non-zero. The report can
still contain the completed observation. Keep the uncertainty and use the owned
kernel observer when confirmation is required; do not retry admission merely to
obtain a zero exit status.

## Cancel without replaying a session

Create a pending admission and privately retain the printed ID:

```sh
kubectl memlens --kubeconfig "$trace_operator_kubeconfig" \
  --context "$trace_operator_context" trace create "$trace_pod" \
  -n "$trace_target_namespace" --container "$trace_container" \
  --kind files --duration 30s --max-events 1000
```

Set `trace_admission` to that exact ID. In the same operator environment, start
watching before the printed pending expiry (at most 15 seconds):

```sh
kubectl memlens --kubeconfig "$trace_operator_kubeconfig" \
  --context "$trace_operator_context" trace watch "$trace_admission" \
  -n "$trace_target_namespace" \
  --export "$trace_output/cancelled-report.json" --confirm-export
```

From a second terminal with the same explicit operator variables, cancel it:

```sh
kubectl memlens --kubeconfig "$trace_operator_kubeconfig" \
  --context "$trace_operator_context" trace cancel "$trace_admission" \
  -n "$trace_target_namespace"
```

Alternatively, Ctrl-C in `run` or `watch` requests cancellation and bounded drain.
Keep the actual cleanup receipt. A 404, timeout or expired handle is not proof of
physical cleanup. Do not automatically create a replacement after an uncertain
response. One admission has one ephemeral reader; `trace export ID` activates a
watch and cannot retrieve an old completed trace. See [client workflows](CLIENT_WORKFLOWS.md).

## Change, disable or remove

Use the [verified lifecycle procedure](RELEASE_LIFECYCLE.md) for upgrades,
emergency disable and uninstall. Retain the previous verified chart and matching
values. Guarded rollback uses `helm upgrade` of that previous set, so its
pre-upgrade checks run; native `helm rollback` is not qualified for this window.

For normal removal, stop creating admissions, cancel or finish known sessions,
and record a private manifest inventory plus the exact owned process/BPF identities
needed for cleanup verification. Then run:

```sh
helm --kubeconfig "$trace_kubeconfig" --kube-context "$trace_context" \
  --namespace "$trace_namespace" uninstall "$trace_release" --wait --timeout 2m
```

Check every inventoried chart resource, APIService, hook and cluster-scoped grant,
then the recorded worker and BPF lifetimes. Follow the bounded
[ownership census procedure](../../prototype/trace/qualification/lifecycle/README.md)
on an authorised qualification host; absence of Pods alone is insufficient.
An unavailable observer leaves kernel cleanup unproven, never confirmed by guesswork.
Repeat the standard memory read and confirm fresh evidence for the same fixture.

Remove separately owned operator/preflight grants, policy and TLS/audit prerequisites
only after checking their recorded UIDs and ownership. Keep shared resources.
Local reports remain operator-owned; server expiry and uninstall do not delete
them. Apply your incident-data retention policy as described in
[audit and retention](AUDIT_AND_RETENTION.md).
