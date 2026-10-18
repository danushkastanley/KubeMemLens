# Verified trace upgrade, rollback and removal

The optional trace extension is a separate, unqualified development profile. Its
release bundle and acceptance policy are separate administrator inputs. The
standard KubeMemLens installation does not acquire tracing capabilities.

## Before changing a release

1. Verify the complete bundle with `hack/trace-release/verify.py`, using the
   expected source/version and caller-owned tool and trust-root pins. Keep its new
   private snapshot and receipt. See [the build contract](RELEASE_BUILD.md).
2. Select the chart from that verified snapshot and the image index named by its
   manifest. Preserve the chart's pinned image defaults. For local tests, import
   the verified OCI archive and check that the runtime reference resolves to that
   exact index; a mutable tag or a similarly named image is insufficient.
3. Review the separate immutable acceptance policy against that set's engine,
   worker hashes, programme index, key and accepted manifests. A bundled signing
   key or a successful signature check does not create installation acceptance.
   Pin the policy bytes in the chart values. Follow
   [worker installation](WORKER_INSTALLATION.md) and the
   [optional chart prerequisites](../../charts/kube-memlens-trace/README.md).
4. Retain the previous verified bundle and its matching policy and configuration.
   Check Node UIDs, architecture, kernel/runtime, seccomp, TLS and control-certificate
   pins for the target installation. Administrator-owned policy/TLS/audit resources
   are not generated or adopted from an untrusted bundle.

Use an explicit kubeconfig, context, namespace and release for every command.
The values file contains references and public pins, never private key bytes or
bearer tokens. Confirm that any existing trace APIService belongs to this release
before changing it; do not adopt another installation's registration.

## Upgrade and guarded rollback

Apply the verified target chart with its matching values. The metadata and host
preflight hooks must succeed before the service workloads change. The API and node
deployments use `Recreate`; active streams can end during the transition. Retain
their actual partial/failure status rather than presenting interrupted work as a
complete trace. An admission identifier from a replaced server cannot be resumed.

For rollback in the current development compatibility window, apply the **previous
verified chart and its matching values using `helm upgrade`**. This reruns the
target chart's pre-upgrade checks. Older charts have no pre-rollback hooks, so
native `helm rollback` and automatic rollback are not qualified procedures for
this window. Do not disable hooks or reuse the newer release's values implicitly.

The command form for either direction is:

```sh
helm --kubeconfig "$trace_kubeconfig" --kube-context "$trace_context" \
  --namespace "$trace_namespace" upgrade "$trace_release" "$trace_target_chart" \
  --values "$trace_target_values" --wait --timeout 5m
```

Set these variables to the exact reviewed installation and verified target before
running the command. Successful Helm completion alone does not prove kernel cleanup.
Confirm target readiness and the configured image/policy, then verify that old
workers and their captured BPF links/maps/programmes have disappeared. Keep the
existing trace deadline and termination grace bounds; an expired trace is not
evidence that an earlier transition cancelled it promptly.

A failed target preflight must leave the previous service image, policy and
deployment identities intact. Inspect the failure and actual release state before
retrying; do not treat a failed Helm history entry as an installed target.

## Emergency disable

Apply the current verified chart with an explicit reviewed values file containing
`enabled: false`. This removes the trace service resources and registration and
prevents new admissions. Verify that active workers and owned BPF objects are gone
and that the standard memory API still serves fresh reads.

Helm can finish an empty-resource update before all old processes have disappeared.
Continue bounded observation of the captured worker/kernel identities after the
command returns. Do not equate an empty Helm render with completed teardown.

Keep administrator-owned policy, TLS and audit prerequisites separately if the
installation will be re-enabled. Re-enabling requires the same verified package,
matching policy and current installation preflight checks.

## Uninstall

Use the explicit installation identity:

```sh
helm --kubeconfig "$trace_kubeconfig" --kube-context "$trace_context" \
  --namespace "$trace_namespace" uninstall "$trace_release" --wait --timeout 2m
```

Verify absence of every chart resource, its APIService and hook Jobs, ConfigMap and
NetworkPolicy, plus captured test-owned worker/BPF identities. Kubernetes object
absence alone cannot establish kernel cleanup. Conversely, do not remove another
workload's BPF objects or claim that the whole host has no BPF activity.

Remove separate administrator-owned prerequisites only when they are no longer
needed and their namespace/resource UIDs still match the installation being
removed. Preserve shared resources and the standard installation. Ordinary image
caches may remain; they are not running workers or attached kernel programmes.

See [local lifecycle evidence](RELEASE_LOCAL_VERIFICATION.md) for the tested scope
and the remaining resource/provider qualification boundary.
