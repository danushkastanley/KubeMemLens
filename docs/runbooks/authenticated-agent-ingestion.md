# Authenticated agent ingestion

Use this runbook when agents cannot publish snapshots, the aggregated API is unavailable, or an agent credential may be compromised.

## Boundary

The default chart sends agent writes through `memory.kubememlens.io/v1alpha1`. The Kubernetes API server authenticates the projected Pod-bound token and forwards verified identity over the aggregation proxy connection. The collector accepts only the configured agent ServiceAccount with one Pod UID, node name, node UID and credential ID claim.

The extension server does not accept bearer tokens directly. It does not trust `X-Remote-*` headers from a direct connection. Removing NetworkPolicy does not remove authentication, delegated authorisation, node binding, epoch or sequence checks.

Projected tokens expire after 3,600 seconds and kubelet refreshes them before expiry. Kubernetes rejects a Pod-bound credential no later than 60 seconds after the Pod deletion timestamp, in addition to the Pod termination grace period. Delete the Pod if its token may be exposed.

## Check the path

```sh
kubectl get apiservice v1alpha1.memory.kubememlens.io
kubectl get --raw /apis/memory.kubememlens.io/v1alpha1
kubectl -n kube-memlens get pods -l app.kubernetes.io/name=kube-memlens-agent
kubectl -n kube-memlens logs daemonset/kube-memlens-agent --tail=50
kubectl -n kube-memlens logs deployment/kube-memlens-collector --tail=50
```

`APIService` must report `Available=True`. Discovery must advertise `ingestionepochs` with `get`, `nodesnapshots` with `create`, and only the read resources and verbs documented in the [tenant read runbook](tenant-scoped-reads.md).

Collector ingestion metrics use fixed result labels. They do not contain usernames, node names, Pod UIDs, node UIDs or credential IDs.

## Sanitised kind qualification result

The final PROD-003 working tree passed the authenticated-ingestion check on a disposable Kubernetes 1.35.5 kind cluster on 25 August 2026. The transcript retained only bounded outcomes:

- the APIService was available and discovery exposed only `get ingestionepochs` and `create nodesnapshots`;
- Service port `8081` was absent, and direct bearer or forged request-header access returned `401`;
- the Kubernetes API server accepted two live credentials bound to one test Pod, while the collector rejected a cross-node claim;
- the first snapshot was accepted, an identical sequence through the rotated credential was an idempotent duplicate, and changed or lower sequences returned `409`;
- per-node rate limiting returned `429`, compressed input returned `415`, a five-MiB body returned `413`, and the concurrency gate passed its pre-read unit test;
- deleting the bound test Pod caused the old credential to return `401`, then the normal agent resumed publishing;
- forced certificate rotation changed the serving certificate through the dual-CA transition while the APIService stayed available; and
- a subsequent authenticated Helm rollback preserved APIService availability, authenticated publishing, absence of port `8081`, and removal of the temporary hook RBAC.

No token, certificate, key, Pod UID, node UID, container path or raw response body was written to the transcript.

## Common failures

| Result | Meaning | Action |
| --- | --- | --- |
| `epoch_mismatch` | The collector restarted after the agent cached its epoch. | The agent refreshes the epoch and retries the same sequence. Check repeated failures for APIService instability. |
| `node_mismatch` | Payload node identity differs from signed Pod claims. | Treat this as a security event. Replace the agent Pod and inspect deployment ownership. |
| `replayed` or `replay_conflict` | A lower sequence or changed duplicate reached the collector. | Replace the agent Pod if the event was not an intentional negative test. |
| `rate_limited` | One authenticated node exceeded its rate or burst. | Check a tight restart loop or changed interval. Do not raise the limit without a measured cold-start test. |
| `concurrency_limited` | The collector reached its bounded decode ceiling. | Check synchronised recovery and collector memory before changing the ceiling. |
| `agent_capacity` or `store_capacity` | A configured in-memory identity or snapshot ceiling was reached. | Check unexpected agents or cluster size. Keep the failure closed. |

## Rotate the serving certificate

The normal upgrade reuses a valid certificate. To rotate it deliberately:

```sh
helm upgrade kube-memlens ./charts/kube-memlens \
  --namespace kube-memlens \
  --reuse-values \
  --set extensionTLS.forceRotate=true \
  --wait

helm upgrade kube-memlens ./charts/kube-memlens \
  --namespace kube-memlens \
  --reuse-values \
  --set extensionTLS.forceRotate=false \
  --wait
```

The hook first trusts the old and new CAs, updates the Secret, waits until the extension presents the new certificate, then removes the old CA from the APIService. It never prints certificate or key material.

## Compromise response

1. Delete the affected agent Pod. Wait for the replacement to become Ready and for the 60-second bound-token invalidation window after the old Pod deletion timestamp.
2. Confirm the old credential now receives `401 Unauthorized` from the Kubernetes API server.
3. Restart the collector to replace the ingestion epoch if captured requests may exist.
4. Rotate the serving certificate if aggregation proxy trust or serving key material may be exposed.
5. Review Kubernetes audit events using the request ID. Do not copy tokens, Secret data or raw operational identifiers into an issue.

## Upgrade and rollback

Upgrade from a published alpha only on a disposable or explicitly authorised evaluation cluster. Confirm the APIService becomes available and new authenticated snapshots arrive before treating the write path as restored.

A normal rollback must target another revision that already uses authenticated ingestion. Verify that Service port `8081` remains absent after rollback. Before v1, restoring the published alpha is permitted only on a trusted evaluation cluster and reopens its known unauthenticated write path. After v1, disable KubeMemLens rather than rolling production back across that boundary.

Uninstall must remove the APIService, TLS Secret, delegated-auth bindings and certificate-bootstrap resources. Existing local incident captures are unchanged.
