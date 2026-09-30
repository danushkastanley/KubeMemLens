# Optional trace chart installation

Status: unqualified development packaging. Resource qualification remains open;
there is no supported or release-qualified node profile. Use only an explicitly
authorised development cluster. The standard chart remains independent.

After installation, use the [bounded CLI and TUI workflows](CLIENT_WORKFLOWS.md).
Their preflight and trace permissions are separate from ordinary memory reads.

## Administrator-owned prerequisites

Use a dedicated namespace with tightly restricted write access. The node process
requires root, `BPF` and `PERFMON`, and five read-only host mounts; namespace policy
must permit this reviewed configuration. Never relax policy for other namespaces.
Use a CNI that enforces Kubernetes NetworkPolicy and verify enforcement locally.

Supply these prerequisites outside Helm ownership:

- A metadata-only Kubernetes audit rule for `tracepreflights`, `traces` and
  `traces/stream`, placed ahead of broader request/response rules. Preflight
  bodies include selected workload identities. See the [audit rule](STREAM.md).
- An immutable, provenance-verified image digest containing the launcher, signed
  worker, accepted programme bundle and retained dependency notices. The chart
  does not publish or approve images.
- An immutable ConfigMap containing `policy.json`, accepted independently of the
  image. Set `acceptancePolicySHA256` to the SHA-256 of those exact file bytes.
  Both services hash the same bounded bytes they parse, including on restart.
- An existing `preflightServiceAccount` in the installation namespace. Its only
  cluster permission is `get` on the exact configured Node resource names. The
  chart creates no hook-owned cluster grants. Keep this account inaccessible to
  tenants; administrators remove its binding separately after final removal.
- Both reviewed Localhost seccomp profiles on each node:
  `kube-memlens-trace/binding-node.json` for host preflight and
  `kube-memlens-trace/filecache-node.json` for the incident runtime. See the
  [runtime profile](FILE_CACHE_PROFILE.md). No runtime profile permits global BPF
  object enumeration.
- TLS Secrets described below. Keep keys out of values, rendered manifests,
  console output and evidence archives. The chart references existing Secrets.

The chart names resources using this prefix:

```text
<first 20 characters of release, trailing hyphen removed>-<first 8 hex characters of SHA256(namespace/release)>-trace
```

Render the chart before creating certificates to confirm the exact Service names.
The API server certificate must cover `<prefix>-api.<namespace>.svc`; each node
server certificate must cover `<prefix>-node-<id>.<namespace>.svc`.

| Reference | Required Secret keys |
| --- | --- |
| `apiTLSSecret` | `tls.crt`, `tls.key`, `node-client.crt`, `node-client.key`, `node-ca.crt` |
| Each node's `tlsSecret` | `tls.crt`, `tls.key`, `control-ca.crt` |

`apiCABundle` is the base64-encoded PEM CA bundle for the aggregated API server.
`controlCertificateSHA256` and each node's `certificateSHA256` are lower-case
SHA-256 hashes of the exact leaf certificate DER bytes. Server and client extended
key usages, current validity, DNS names, chains, API/control key pairs and both pin
directions are checked before installation. Node private keys are not mounted in
that verifier; each node validates its own certificate/key pair before serving.
Rotate certificates through new immutable Secrets and a reviewed values change.

## Values and preflight

Set `enabled: true`, `profile: development-linux-containerd` and
`acknowledgeUnqualifiedDevelopment: true`. Supply the prerequisites above and
`image.repository`, `image.digest` and the reviewed pull policy. Values schema
rejects unknown configuration and arbitrary arguments.

Declare one to 64 nodes, each with a unique `id`, `name` and `uid`, plus
`architecture`, `kernelVersion`, `runtimeVersion`, `tlsSecret`,
`certificateSHA256` and `kubeletCgroupRoot`. Architecture is `amd64` or `arm64`;
runtime is the exact reported `containerd://...` version. Read current Node
metadata instead of guessing these values. Confirm the kubelet cgroup root on
each host. These declarations do not qualify either architecture or kernel.

Pre-install and pre-upgrade hooks run in order:

1. The bounded installation specification and host-probe network policy are
   created.
2. An unprivileged Job validates policy, certificate trust and fresh Node metadata,
   including UID, Linux architecture, exact kernel/runtime versions and readiness.
3. One bounded host Job per node runs `doctor --json --timeout=10s` with the
   baseline seccomp profile, no service-account token and no network. It creates
   and closes baseline BPF test objects; it does not attach incident programmes.
4. Helm creates or updates the application resources only after hooks succeed.

Jobs have no retries, explicit deadlines and a five-minute finished-job TTL.
Successful hooks are removed when the hook sequence succeeds. A later hook
failure may leave earlier completed Jobs until TTL; inspect failed installations
and remove only verified release-owned hook objects. Do not use `--no-hooks`.

The API uses pinned profile mode. Its existing fresh Node reads reject profile
drift during admission, stream revalidation and OOM context acquisition. Node
startup also compares the actual kernel release with the declared release before
opening its listener. A node replacement or runtime upgrade requires updated
reviewed values and successful preflight. The older manual prototype's baseline
mode remains separate; it cannot silently ignore supplied profile pins.

## Install and access

Only one installation can own `v1alpha1.tracing.kubememlens.io`. Inspect existing
ownership first. Do not overwrite or adopt an existing prototype or Helm release
without an explicit migration and restore plan.

With the context, namespace and reviewed values selected explicitly:

```sh
helm upgrade --install "$TRACE_RELEASE" charts/kube-memlens-trace \
  --kube-context "$TRACE_CONTEXT" --namespace "$TRACE_NAMESPACE" \
  --values "$TRACE_VALUES" --wait --timeout 5m
```

The API gets Pods, exact registered Nodes and delegated SubjectAccessReviews. It
reads the aggregation request-header configuration through a separate RoleBinding
in `kube-system`. Node processes receive no Kubernetes token or API permissions.
No tenant or operator binding is created. Bind the generated operator ClusterRole
only to named operators using namespace-scoped RoleBindings where appropriate;
verify both allowed and denied identities through the real aggregated API.
The role includes `create tracepreflights` for the explicit inspection workflow.
Preflight also requires trace creation and the exact Pod read; it creates no
session and does not replace authorisation at admission or streaming time.

API ingress permits port 8443 so the aggregation layer can reach it across CNI
topologies. Its egress is not restricted by this chart: it needs the Kubernetes
API, DNS and registered node services. Node ingress permits only this release's
API Pods on 9443; node-initiated egress is denied. Mutual TLS and leaf pins remain
required. Namespace writers can change labels and trust resources, so namespace
administration is a trust boundary, not a tenant privilege.

## Upgrade, rollback and removal

Stop starting traces and let active sessions finish before changing the release.
Retain the previous image digest, immutable policy/TLS references and values.
Use the same explicit context and namespace for upgrades. Recreate deployments
avoid overlapping replicas; they still interrupt service while replacing Pods.

Helm rollback does not run pre-upgrade hooks. To reapply an earlier configuration
with these checks, perform `helm upgrade` with the retained chart and values.
Reject a rollback when prior node identities, policies or certificates are stale.
Do not infer compatibility from a previous successful installation.

Before removal, retain a manifest inventory, verify no active workers and record
owned kernel objects when exercising active teardown. Run `helm uninstall` with
the same explicit context and namespace. Verify removal of namespaced workloads,
Services, registry and network policies, the APIService, generated ClusterRoles
and ClusterRoleBinding, and the request-header RoleBinding in `kube-system`.
Wait for owned Pods and their original processes to terminate; Helm may return
before asynchronous Pod termination finishes. Inspect hook Jobs and their Pods
too. Finalisers or surviving resources make the
cleanup incomplete. Administrator-owned namespace, policy, TLS and preflight
account intentionally remain; remove them separately only after verifying scope.

Pod disappearance is not proof of kernel cleanup. For local lifecycle evidence,
use the independently privileged
[ownership census](../../prototype/trace/qualification/lifecycle/main_linux.go):
identify the exact container, parent PID/start time and executable hash; snapshot
only its accepted workers and selected fixture cgroups while active; then check
only those recorded map/programme/link IDs after teardown. It never globally
enumerates or deletes BPF objects. Permission failure or missing ownership
evidence is an incomplete check. This chart does not grant an in-cluster cleanup
verifier global BPF privileges or claim that Helm success proves kernel cleanup.

See [worker acceptance](WORKER_INSTALLATION.md),
[admission](ADMISSION.md) and [lifecycle qualification](LIFECYCLE_LOCAL_QUALIFICATION.md)
for the runtime contracts and remaining evidence boundaries.
