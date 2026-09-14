# Optional trace admission prototype

This separate module provides `memlens-trace doctor`. It runs bounded,
non-attaching Linux feature probes for the accepted engine baseline. A supported
result does **not** approve incident tracing or custom programmes. See the
[preflight contract](../../docs/ebpf/PREFLIGHT.md) and
[engine acceptance](../../docs/ebpf/ENGINE_CONTRACT.md).

The standard agent, collector, doctor, chart and release artefacts do not include
these commands, their dependencies or their privileges. The optional image also
provides `admission-api` and `binding-node` for short-lived, immutable target
admission. They open separate authenticated listeners only when explicitly
invoked. See [admission installation and verification](../../docs/ebpf/ADMISSION.md).
There is no runtime socket, host PID namespace or incident programme.

## Local build and checks

From the repository root:

```sh
make check-trace-preflight
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go -C prototype/trace build \
  -trimpath -o /tmp/memlens-trace ./cmd/memlens-trace
docker build -f prototype/trace/Dockerfile -t kube-memlens-preflight:local .
```

Use `amd64` when that is the native Linux node architecture. A cross-build is
not runtime qualification. The separate module uses cilium/ebpf v0.22.0 and
x/sys v0.47.0; it does not yet import the Inspektor Gadget SDK. These libraries
encode fixed probes and parse kernel metadata. No gadget is executed.

The scratch image runs as UID 65532 by default. Invoking it without the explicit
administrator profile reports unavailable permissions; it cannot elevate itself.
The Kubernetes Job deliberately selects UID 0 to read protected kernel metadata.
Local Docker qualification must likewise specify `--user 0:0`, together with the
reviewed capabilities, seccomp policy and resource limits.

The scratch image contains Go and the licence/notice files of every Go module
imported by the optional binary. The build derives `/licences/inventory.json` from
`go list -deps` and fails if a module has no licence file. Kubernetes adapters use
the same pinned versions as the main module; upstream gadget references remain
evaluation inputs.
The embedded fixed probe instruction sequences are authored in this module;
there is no upstream gadget bytecode embedded in the binary.

## Administrator-only Kubernetes check

Use a disposable local cluster. The manifests deliberately grant no deployment
rights: an existing cluster administrator installs the namespace and creates
one Job for each selected node. `kube-memlens-trace-admins` can read its Jobs,
Pods and logs, including their node association. Ordinary namespace roles and
the worker ServiceAccount receive no such access. The latter has no token.

Before running, the administrator must:

1. Build and load the image, retain its immutable digest and create the matching
   digest alias in the local container runtime. `imagePullPolicy: Never` prevents
   a registry fallback. kind's image import may preserve only the tag; add the
   matching digest alias with `ctr -n k8s.io images tag` on each test node.
2. Install the exact [seccomp policy](seccomp/preflight.json) under each node's
   kubelet seccomp root as `kube-memlens-trace/preflight.json`, root-owned and
   unwritable by tenants. No daemon installs or relaxes it at runtime.
3. Make existing kernel tracing, BTF, securityfs and bpffs metadata available at
   their normal paths. The Job mounts those directories read-only. Missing data
   is reported explicitly; the command never mounts a filesystem itself.
4. Reserve this namespace for trusted administrators. Its Pod Security exception
   permits the two explicit capabilities and metadata mounts. The actual Pod
   remains non-privileged with no escalation, host namespaces or writable root.

```sh
kubectl --context YOUR_LOCAL_CONTEXT apply -f prototype/trace/kubernetes/namespace.yaml
python3 prototype/trace/kubernetes/render_job.py \
  --node YOUR_NODE --name preflight-one \
  --image YOUR_LOCAL_IMAGE@sha256:YOUR_VERIFIED_DIGEST > /tmp/preflight-job.json
kubectl --context YOUR_LOCAL_CONTEXT apply -f /tmp/preflight-job.json
kubectl --context YOUR_LOCAL_CONTEXT -n kube-memlens-trace-preflight \
  logs job/preflight-one
```

The renderer rejects mutable images. Do not substitute an unreviewed image or
security profile to obtain a supported result. Exit 0 means baseline supported;
exit 2 means degraded, unsupported or invalid invocation. Kubernetes will mark
a nonzero Job as failed; read its report for the specific cause. Reports contain
no incident events. Job and Pod metadata/logs expire after five minutes; collect
qualification evidence into a protected local directory if needed.

The default deadline is ten seconds, with a maximum of fifteen. A fresh worker
process performs serial probes. The parent kills and reaps an overdue worker.
The Job adds a thirty-second active deadline, no retry, a 256 MiB memory limit
and two-CPU ceiling. The separate admission service bounds its own node work; these standalone
administrator Jobs do not participate in its quotas. Administrators must not
launch concurrent diagnostic campaigns on one node. The local Docker qualification also sets 64 PIDs, 64 descriptors and an
8 MiB memlock ceiling. Kubernetes inherits these node/runtime limits; it does
not offer per-Pod fields for all of them.

## Ownership and local qualification

The worker audits its own descriptors and an optional private supervisor receipt.
The one-shot Job has no shared receipt mount, since it has no previous incident
worker to recover. Reserved pins are treated as uncertain state and never
removed. Read the contract before supplying a supervisor receipt in a later
runtime integration.

The [qualification tests](qualification/inventory_linux_test.go) include an
explicitly gated global census for an administrator on an owned disposable Linux
node. Linux requires SYS_ADMIN for this census. The test binary is not part of
the image, command or deployment. Never grant that capability to the worker.
Live descriptor and timeout tests in `probe/` also require explicit test opt-in;
ordinary `make check` does not load BPF. See the contract for evidence boundaries.

To roll back, delete only the test Jobs and namespace you created, remove this
prototype's seccomp file from the test nodes, and discard the local image.
Remove administrator-created mounts by destroying the disposable test cluster.
No standard chart upgrade or data migration is required.
