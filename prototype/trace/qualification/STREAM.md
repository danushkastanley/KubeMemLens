# Local stream qualification

This procedure uses a **test executable** with a deterministic memory engine.
It verifies transport and ownership through real Kubernetes aggregation. It does
not execute an incident programme or qualify file/cache/OOM kernel semantics.
Production entrypoints always supply no runtime and no programme allowlist.

Use an owned two-node kind cluster named
`kube-memlens-node-context-r6-stream`, Kubernetes 1.36.1 at
`kindest/node:v1.36.1@sha256:3489c7674813ba5d8b1a9977baea8a6e553784dab7b84759d1014dbd78f7ebd5`.
Use a protected dedicated kubeconfig with context
`kind-kube-memlens-node-context-r6-stream`. Never point this procedure at a shared
or managed cluster. For network-policy qualification disable kind's default CNI
and use the pinned Cilium installer in `hack/node-qualification/cilium_kind.py`.
Configure metadata-only auditing for the tracing API group before cluster
creation; mount the audit policy separately from credentials. See
[the stream contract](../../../docs/ebpf/STREAM.md).

## Build and install

Run from the repository root, selecting the actual local Linux architecture:

```sh
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go -C prototype/trace test -c \
  -o /YOUR/PROTECTED/stream.test ./cmd/memlens-trace
```

Build a local scratch image with that executable at `/memlens-trace`, and copy
`prototype/trace/testdata/reference` to `/opt/memlens-trace/reference`. Set
`USER 65532:65532` and `ENTRYPOINT ["/memlens-trace"]`. Load the image into the
owned kind cluster and select its exact immutable digest. Record the source
commit, whether it is dirty, source-file SHA-256 manifest and binary/image digest.
The test image must never enter normal release publication.

Follow [the admission installation procedure](../../../docs/ebpf/ADMISSION.md),
substituting the owned stream context and image. Keep the node security profile,
seccomp file, supervised preflight and pinned mutual TLS unchanged. In the
**private rendered fixture manifest only**, add the following environment entry
to the controller and node containers:

```yaml
- name: KML_STREAM_QUALIFICATION
  value: owned-local-kind-contract-fixture
```

This test mode permits immutable path consent and allows only its fixed fixture
digest. Do not add this environment entry to production manifests. A normal
`go build` excludes both the test adapter and the activation marker.

Create namespaces `trace-target-a` and `trace-target-b`, each with a running Pod
`target`, container `worker`, on the registered worker node. Use ServiceAccount
`tenant` in both namespaces and `colleague` in A. Bind their namespace Role
`trace-test` with separate rules for:

- `create`, `get`, `delete` on `traces` in `tracing.kubememlens.io`;
- `get` on `traces/stream` in that group;
- `get` on core `pods` with `resourceNames: [target]`.

## Exercise and retain evidence

```sh
python3 prototype/trace/qualification/stream_api.py \
  --kubeconfig /YOUR/PROTECTED/LOCAL_KUBECONFIG --owned-fixture r6-stream \
  --output /YOUR/PROTECTED/stream-result.json
```

The runner checks the exact context and localhost HTTPS endpoint, obtains real
short-lived tenant tokens without printing them, and verifies frame/accounting
bounds, a 12-second stream, owner/consumer isolation, cancellation, consent and
permission revocation. It temporarily removes only the fixture stream Role rule,
restores it in `finally`, and cancels each admission. Its result contains no raw
events, paths or credentials.

For Cilium enforcement build the `./qualification` test executable and run
`TestLiveStreamNetworkPolicy` in owned probe Jobs. Set
`KML_NETWORK_QUALIFICATION=owned-local-kind-cni`, `KML_NETWORK_ENDPOINT` to the
literal Service IP and port, and `KML_NETWORK_EXPECT` to `connected` or `denied`.
Test the same-namespace controller label to node port 9443 (connected), wrong
label (denied), same label from the other namespace (denied), and node-labelled
initiated egress to API port 8443 (denied). Denied means a connection timeout;
connection refusal does not prove policy enforcement. Delete probe Jobs.

Inspect actual API-server audit records for tracing requests: level `Metadata`,
no request/response objects, and no `CONTRACT-FIXTURE-ONLY` marker. Check controller
and node logs for the same marker. Inspect logs locally; retain only sanitised
assertion results. Record target deletion and node-connection failure separately:
healthy downstream connections must end incomplete, never with fabricated counts.

A node restart must not confirm predecessor cleanup. Verify that another admission
from that owner is refused while cleanup is uncertain. Recovery is administrative:
prove the old owned process/resources are gone before a controlled controller
restart. Never infer cleanup solely from a new process's empty map. Independent
watchdog/restart qualification with real programmes belongs to BPF-007.

## Cleanup

Stop admissions and verify owned streams terminate before deleting the rendered
API resources. Delete only this owned kind cluster; confirm its node containers
are absent and unrelated clusters remain. Remove its protected kubeconfig,
certificates and Secret manifests. Retain sanitised assertions, source/image
identities and cleanup evidence. No managed-provider or R7 claim follows.
