# Tenant isolation incident response

Use this runbook when a KubeMemLens principal may have read another tenant, an agent identity may be compromised, or a direct listener returns diagnostic data.

## Contain first

1. Stop affected reads by deleting the specific namespace `RoleBinding` or cluster `ClusterRoleBinding`. Do not edit the shared viewer roles during an incident.
2. Revoke the metrics reader binding if workload metrics may have crossed scope. Also stop or quarantine downstream Prometheus retention that received the data.
3. Delete a compromised tenant or agent Pod. A Pod-bound token becomes invalid when Kubernetes observes deletion.
4. Restart the collector Deployment if an ingestion request may have been captured. Restarting changes the in-memory ingestion epoch and rejects requests from the old process.
5. If the aggregation proxy trust or serving key may be exposed, follow the certificate rotation procedure in [authenticated agent ingestion](authenticated-agent-ingestion.md). Keep the APIService unavailable until the new CA bundle and serving certificate agree.
6. If direct collector reads, writes or workload metrics are reachable, scale the collector Deployment to zero. Do not enable the legacy listener as a fallback.

Example revocation checks:

```bash
kubectl delete rolebinding kube-memlens-namespace-viewer -n <tenant-namespace>
kubectl delete clusterrolebinding <metrics-or-cluster-reader-binding>
kubectl auth can-i --as=<principal> list pods.memory.kubememlens.io -n <tenant-namespace>
kubectl auth can-i --as=<principal> get metrics.memory.kubememlens.io
```

## Preserve safe evidence

Record UTC time, Kubernetes audit request ID, verb, KubeMemLens resource, namespace scope, decision and bounded reason code. Preserve the Helm release revision, image digest, chart version, APIService condition and collector restart count.

Do not place these values in an issue, chat transcript or retained test bundle:

- bearer tokens, kubeconfig users or client certificates;
- raw group lists or forwarded identity headers;
- Pod or node UIDs, container IDs or cgroup paths;
- label maps or denied object names; or
- unredacted collector, agent or API server logs.

Quarantine existing incident captures until their scope is known. The default capture is redacted but still contains authorised workload display names and memory evidence. Delete a capture through the organisation's approved secure deletion and retention process when it is no longer needed.

Use Kubernetes audit records to correlate a request. KubeMemLens security logs deliberately omit the raw principal, namespace and object name. Do not widen log verbosity during containment.

## Check the boundary

After containment, verify each relevant point:

```bash
kubectl get apiservice v1alpha1.memory.kubememlens.io
kubectl get service kube-memlens-collector -n kube-memlens -o jsonpath='{.spec.ports[*].port}'
kubectl auth can-i --as=system:serviceaccount:kube-memlens:kube-memlens-collector get pods --all-namespaces
kubectl auth can-i --as=system:serviceaccount:kube-memlens:kube-memlens-collector get secrets -n kube-memlens
```

The Service must expose only port `443`. The collector identity must not read Pods, Nodes, Secrets, workloads or KubeMemLens tenant data. Pod-local `:8080` must serve health only. Direct TLS with a bearer token or forged `X-Remote-*` header must not authenticate.

Run the disposable-cluster validation from [tenant isolation validation](../security/tenant-isolation-validation.md) against the candidate fix. Never remove a production NetworkPolicy to reproduce an incident.

## Recover

1. Apply the smallest reviewed fix and rotate any exposed serving material.
2. Restore the collector and wait for the APIService, Deployment and DaemonSet to report ready.
3. Recreate only the required namespace or metrics binding.
4. Confirm the affected principal can read its intended scope and cannot read another namespace, cluster resources or metrics without a separate grant.
5. Confirm fresh agent writes use the new collector epoch and that the collector has no unexpected restart.
6. Re-enable downstream scraping only after its binding and retained data have been reviewed.

Close the incident only after the finding register names the cause, owner, fixing commit, verification command and residual risk. A NetworkPolicy-only fix is insufficient because the Kubernetes authentication and authorisation boundary must still hold when the policy is absent.

## Roll back

If the fix cannot restore the boundary, keep KubeMemLens disabled. Before v1, a trusted single-tenant evaluation cluster may reinstall the published alpha under its documented limitations. A shared cluster must never roll back to unauthenticated collector reads or writes.
