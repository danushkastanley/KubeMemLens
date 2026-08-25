# Collector reliability

Use this runbook when KubeMemLens reports `rebuilding`, `degraded`, `stale` or `unavailable`, when the collector Pod is not ready, or after an agent, node or collector rollout.

The collector is single-replica and in-memory. Restarting it always discards current snapshots, event-delta baselines and bounded history. Read the [reliability contract](../reliability.md) before choosing a restart as the repair.

## Establish the state

Run these checks with the caller identity used by the affected workflow:

```sh
kubectl get apiservice v1alpha1.memory.kubememlens.io
kubectl get pods -n kube-memlens -o wide
kubectl memlens status
kubectl memlens doctor --strict
kubectl get --raw /apis/memory.kubememlens.io/v1alpha1/clusterstatus/current
```

The cluster-status read requires the explicit cluster-viewer role. A namespace viewer should use a namespaced `top pods` or Pod history request instead. Do not grant cluster access to make diagnosis easier.

Record these fields before changing anything:

- collector `state`, `generation`, `startedAt` and `transitionedAt`;
- `freshNodes`, `staleNodes`, `lastSnapshotAt` and `lastReceivedAt`;
- history `resetAt`, `availableFrom`, `completeness`, `droppedSeries`, `evictedPoints` and `lastLossAt`;
- affected node names and agent Pod readiness; and
- the collection timestamp on one affected row.

Use UTC timestamps. Do not paste raw tokens, certificates, Pod UIDs, container IDs, cgroup paths or unredacted responses into an incident ticket.

## Check component health

Kubernetes reports readiness through the extension HTTPS endpoint:

```sh
kubectl describe pod -n kube-memlens -l app.kubernetes.io/name=kube-memlens-collector
kubectl logs -n kube-memlens deployment/kube-memlens-collector --tail=100
```

Use the Pod probe events and `APIService` condition to diagnose extension readiness. Do not expose Pod-local health ports through a Service.

Interpret the probes separately:

- liveness success with readiness failure points to request-header configuration or delegated `SubjectAccessReview` connectivity;
- readiness success with `rebuilding` means the serving boundary works but no agent has populated this collector generation;
- readiness success with `degraded` or `stale` means the serving boundary works but evidence coverage does not; and
- an unavailable client may be a denied caller, a failed `APIService` or a network failure. Check RBAC before treating it as a collector process fault.

## Rebuilding after install, restart or upgrade

Check both rollouts and agent posts:

```sh
kubectl rollout status deployment/kube-memlens-collector -n kube-memlens
kubectl rollout status daemonset/kube-memlens-agent -n kube-memlens
kubectl logs -n kube-memlens daemonset/kube-memlens-agent --tail=100
kubectl logs -n kube-memlens deployment/kube-memlens-collector --tail=100
```

Confirm that agents log a successful post after the current collector `startedAt`. Then repeat `kubectl memlens status` and check that `lastSnapshotAt` moved forward.

History remains partial after current evidence returns. Record the new generation and `history.resetAt`. Do not describe pre-restart history as recovered.

## One stale or missing agent

List the DaemonSet placement:

```sh
kubectl get daemonset kube-memlens-agent -n kube-memlens
kubectl get pods -n kube-memlens -l app.kubernetes.io/name=kube-memlens-agent -o wide
kubectl get nodes
```

For the affected node:

1. Check whether the node is Ready and schedulable.
2. Check the agent Pod phase, restart count, events and bounded logs.
3. Check Pod metadata cache sync, cgroup v2 mount access and the last snapshot post result.
4. Check `APIService` availability and authenticated ingestion rejection counts.
5. Restore the node or agent. Wait for a successful post with a collection timestamp newer than the fault.

Do not use a stale row to make a current memory decision. Other fresh nodes remain usable, but the cluster view is partial.

## Kubernetes API or delegated authorisation outage

Check the aggregation and authorisation dependencies:

```sh
kubectl get apiservice v1alpha1.memory.kubemlens.io -o yaml
kubectl get configmap extension-apiserver-authentication -n kube-system
kubectl auth can-i create subjectaccessreviews.authorization.k8s.io \
  --as=system:serviceaccount:kube-memlens:kube-memlens-collector
kubectl logs -n kube-memlens deployment/kube-memlens-collector --tail=100
```

Do not edit the request-header ConfigMap. The Kubernetes API server owns it. Restore API connectivity or the collector ServiceAccount's existing delegated-auth binding. Do not widen permissions beyond the chart's reviewed role.

The collector should remain live and become not ready after its cached probe observes the outage. After repair, wait for readiness and a new agent collection timestamp. A ready API with old rows is not full recovery.

## Partial agent rollout

```sh
kubectl rollout status daemonset/kube-memlens-agent -n kube-memlens --timeout=2m
kubectl get pods -n kube-memlens -l app.kubernetes.io/name=kube-memlens-agent -o wide
kubectl memlens status
```

During a partial rollout, expect a degraded state once an old node record becomes stale while another remains fresh. Check unschedulable nodes, custom taints, image pull errors and admission denials. Do not add blanket tolerations or privileged access to force completion.

Recovery requires one fresh, complete snapshot from every intended node record. Compare collection times with Pod creation times carefully. They answer different questions.

## Node replacement

The old node record remains as stale last-known evidence while Kubernetes still reports that Node. A replacement node has a different node identity and starts as missing coverage until its agent posts.

1. Confirm that Kubernetes removed or cordoned the old node as intended.
2. Confirm that the DaemonSet scheduled on the replacement node.
3. Wait for metadata cache sync and a successful authenticated post.
4. Confirm the replacement record is fresh.
5. Confirm the next successful Node inventory refresh removes the old record and reports the replacement as expected.

The collector returns to ready when every selected Node is fresh and complete. It returns to rebuilding if no evidence remains. Do not restart the collector merely to remove an old record. Fix Node inventory access or remove the Node through the normal Kubernetes lifecycle; a collector restart also removes all current evidence and history.

## Capacity and partial history

Check `kubectl memlens status` and the authenticated metrics resource for current store ceilings, ingestion rejections and history loss.

- `droppedSeries` means the history series ceiling rejected a new Pod instance.
- `evictedPoints` means the per-series point ceiling removed older points.
- `completeness: partial` means the requested history window is not complete.

History completeness may return to complete after the last capacity loss ages outside the configured window. Cumulative loss counters do not reset until the collector generation changes.

Do not raise limits during an incident without a measured memory and response-size check. Current-state capacity rejection and history loss need different actions. See [metrics](../metrics.md) for the exposed counters and [installation](../installation.md) for configured bounds.

## Verify recovery

Recovery needs more than green Pods:

```sh
kubectl get apiservice v1alpha1.memory.kubememlens.io
kubectl rollout status deployment/kube-memlens-collector -n kube-memlens
kubectl rollout status daemonset/kube-memlens-agent -n kube-memlens
kubectl memlens status
kubectl memlens doctor --strict
```

Confirm all of the following:

- the API and TUI report the expected state;
- every intended node has a fresh snapshot;
- the affected collection timestamp is newer than the repair;
- ingestion rejection rates returned to their baseline;
- the collector generation did not change unexpectedly; and
- any history reset, dropped series, evicted points or `lastLossAt` change is recorded.

For a failure test, record fault time, first failure-state time, repair time and first ready time. Use the same clock and keep the output sanitised.

## Roll back

Use a reviewed Helm revision. Rollback restarts the collector and loses its in-memory state.

```sh
helm history kube-memlens -n kube-memlens
helm rollback kube-memlens <revision> -n kube-memlens --wait
kubectl rollout status deployment/kube-memlens-collector -n kube-memlens
kubectl rollout status daemonset/kube-memlens-agent -n kube-memlens
kubectl memlens status
```

Verify that the authenticated `APIService` is available, Service port `8081` is absent and a new agent snapshot arrives. Roll back only to a revision that preserves authenticated ingestion and tenant-scoped reads. If no such revision exists, disable KubeMemLens instead of restoring an unauthenticated path.

There is no data restore for collector snapshots or history. Local incident captures and an operator's Prometheus data are separate and remain under that operator's retention policy.
