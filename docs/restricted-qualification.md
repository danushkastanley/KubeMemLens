# Restricted provider qualification

Restricted qualification uses an existing authorised Pod in an explicitly
selected context. It creates no Kubernetes workload, host mount, cloud resource
or RBAC binding. SelfSubjectAccessReview requests report the caller's access;
each capture still authorises its own reads. Missing metrics and denied optional
Node access are recorded without widening permissions.

Profiles are separate: `local-kind`, `gke-autopilot`, `eks-fargate` and
`aks-virtual-nodes`. All managed profiles currently remain **unqualified**.
Existing deep-mode unsupported records are unchanged. A passing local fixture
does not establish provider Metrics API availability or measurement accuracy.

## Prepare and run

Before a managed-provider run, obtain owner approval for the exact account,
context, namespace, existing test workload and cleanup scope. Recheck the
provider's current official documentation for Metrics API and Node visibility.
If a disposable environment is required, approve its creation and cleanup
separately; this script does neither.

Build the intended binary, then run:

```sh
python3 hack/restricted-providers/qualify.py \
  --profile gke-autopilot \
  --cli ./bin/kubectl-memlens \
  --kubeconfig /private/path/config \
  --context approved-context \
  --namespace approved-test-scope \
  --output restricted-result.json
```

Repeat independently with the approved Fargate and virtual-node contexts. Use
the actual build output path. The script does not retain context names, workload
names, credentials, kubeconfigs or raw API errors in its output. Temporary
captures and access-review documents are private and removed at exit. The
result file is created exclusively with mode `0600`.

## Evidence and acceptance

The bounded JSON record includes the binary SHA-256 digest, exact control-plane
version, served source API versions, availability/freshness/completeness,
permission decisions, capture visibility counts, unavailable operations and
cleanup assertions. It exercises capture and repeated offline replay, then
compares the captured Pod with itself to verify the offline path. This last
check proves processing compatibility, not change detection under real load.
Node rows are omitted from namespace captures; Node visibility is recorded by
the source reports. Named-resource permissions may be narrower than the broad
Node GET decision recorded by the access review.

The existing provider privacy scanner rejects identifier-bearing result text.
Output is `observed` when at least one Pod has working-set data (including zero)
and `limited` when none does. `providerSupportQualified` stays false until a
reviewer accepts the complete profile evidence, scope and limitations. Neither
outcome automatically changes the public support matrix.

For provider acceptance, retain the reviewed digest/version record, validate
the required UI/CLI workflows and resource visibility for that provider, record
partial/unavailable operations, and verify cleanup of any separately approved
disposable infrastructure. Update only that provider's row in
[compatibility](compatibility.md). Requalify after control-plane, metrics
provider, permission or binary changes that affect the workflow.

The local kind harness invokes this same adapter alongside CLI/PTY checks with
an isolated caller and controlled TLS Metrics API. It installs only its
disposable local API fixture and removes it afterwards. It leaves existing
clusters untouched and makes no managed-provider support claim.
