# Kubernetes observation for Node-context qualification

The observer reads the production collector API, component loopback metrics and
read-only cgroup counters through an explicit Kubernetes context. It requires no
SSH access, host PID namespace or host network namespace. It does not grant
provider qualification or approve a provider run.

## Observation footprint

`hack/node-qualification/observer_specs.py` defines the fixed probe resources:

- One DaemonSet Pod per selected Node mounts `/sys/fs/cgroup` read-only. It has
  no ServiceAccount token and denies all Pod ingress and egress. It reads the
  isolated kubelet cgroup and the exact producer container cgroup. Missing or
  ambiguous cgroups, counter resets and changed identities fail observation.
- An ephemeral container in each agent Pod reads metrics on loopback port 8082.
- An ephemeral container in each producer Pod reads metrics on loopback port
  8083 and hashes its existing projected kubelet token. Only the in-memory hash
  is compared; no token or hash belongs in shared evidence.

All probes use UID/GID 65532, drop every Linux capability, prohibit privilege
escalation, use a read-only root filesystem and request RuntimeDefault seccomp.
Their image is the profile's pinned BusyBox workload image. Commands terminate
after two hours. Kubernetes prohibits resource limits on ephemeral containers;
their resource use draws on the existing Pod's available resources. The host
probe requests 1m CPU and 4 MiB memory with a 16 MiB memory limit.

The installer refuses existing ephemeral containers. It verifies the namespace
and target Pod UIDs, keeps the original Pod/container identity, and updates the
ephemeral-container subresource using the current resource version. Admission
denial fails the run. Do not change Pod Security, IAM or provider policy to make
an observer start without separate approval.

The host probe and agent observer must remain present in both measurement
phases. The producer observer exists only while the optional producer is
enabled. Record this instrumentation separately from the existing Docker-based
kind measurements. The extra probes may affect measured cost.

## Production API transport

`hack/node-qualification/api-bridge/` uses the existing Go Kubernetes client.
It negotiates snapshot schema 3 and preserves kubeconfig authentication,
including credential refresh. It accepts only status, namespace-scoped container
reads, a single Node read and bounded Node history. An explicit kubeconfig,
context and namespace are required. HTTP, insecure TLS and URL credentials are
rejected. Output and process duration are bounded; command diagnostics remain
private and are not copied into evidence.

Executing an approved provider context can invoke its configured credential
plugin. The offline proposal compiler does not execute this bridge or load
Kubernetes credentials.

## Local integration check

Run the observer against an owned disposable kind fixture:

```sh
NODE_CONTEXT_ACKNOWLEDGE=create-and-remove-node-context-kind \
NODE_CONTEXT_VERIFY_INGESTION=true \
NODE_CONTEXT_OBSERVER_PROFILE=hack/node-qualification/profiles/kind-137.json \
NODE_CONTEXT_ARTIFACT_DIR=/absolute/path/to/new-evidence \
hack/verify-node-context-kind.sh
```

For Kubernetes 1.36, select `kind-136.json` and its exact `nodeImage` value through
`NODE_CONTEXT_NODE_IMAGE`. The profile and fixture image must match. The check
cannot be combined with the full qualification or lifecycle diagnostic mode.

The fixture exercises the existing direct TLS/RBAC, authenticated ingestion,
CLI/TUI, restart and rollback checks. It also installs the observer during the
disabled baseline and reads two samples per phase. It verifies workload mapping,
charged memory, CPU deltas, acquisition progress and Node API/history reads.
The parent removes its cluster and fixture image and then confirms cleanup in
the bounded observer records.

This short check verifies the adapter. It does not measure the full fixed
qualification windows, prove natural token rotation or establish provider
support. Provider orchestration, approval, full measurements, replacement,
cleanup and independent review remain required under the
[qualification protocol](node-context-qualification.md).
