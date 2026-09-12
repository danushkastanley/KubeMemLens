# Run an approved Node-context provider qualification

The provider command is `hack/node-qualification/run_provider.py`. It connects
the candidate verifier, current inventory, fixed measurement windows, recovery
checks, operator replacement observation, NetworkPolicy controls and production
CLI checks. It does not create, replace or delete provider infrastructure.
A successful command still leaves independent provider cleanup and qualification
review pending. No provider support claim follows from the tooling alone.

## Before the run

Prepare and review the exact [private proposal](node-context-provider-preparation.md)
from a clean, immutable tool checkout. The owner must approve that proposal,
account/context, disposable pool, candidate artefacts, trust/audience settings,
observation footprint, replacement operation and cleanup plan. The acknowledgement
arguments bind an operator invocation; they do not create that approval.

Use a signed candidate containing the Node-context implementation and its exact
reproducible OCI archive. The earlier RC2 verifier diagnostic is not a candidate
for this feature. Candidate production and publication follow the separate
[release process](release-process.md); this command never publishes artefacts.

The host needs the existing Go toolchain, Python, Git, Helm, kubectl, cosign, gh
and the selected provider's read-only inventory CLI. Existing credentials stay
outside the proposal and evidence. The explicit kubeconfig may use its configured
credential plugin after run approval. The command never changes provider IAM,
firewalls, CNI configuration or control-plane settings.

The output must be a new absolute directory with an existing parent. An output
inside the repository must be ignored by Git. The command creates private
`private/` and `evidence/` directories with mode `0700`; evidence files use `0600`.
Keep the whole output directory outside commits and uploads until the intended
sanitised evidence has been reviewed.

## Invocation

Supply the digest and candidate tag from the completed review:

```sh
python3 hack/node-qualification/run_provider.py \
  --proposal /absolute/private/proposal \
  --profile hack/node-qualification/profiles/gke-standard.json \
  --plan-digest "${reviewed_plan_digest}" \
  --candidate-bundle /absolute/private/candidate \
  --candidate-tag "${reviewed_candidate_tag}" \
  --image-archive /absolute/private/candidate-image.tar \
  --architecture amd64 \
  --output-dir /absolute/private/new-provider-run \
  --replacement-slot 0 \
  --acknowledge run-reviewed-node-context-plan \
  --replacement-acknowledge provider-action-approved
```

Select the canonical EKS, AKS or self-managed profile when that is the approved
run. Architecture must match both the selected inventory profile and the live
pool. Slot numbers follow the initial Node binding order. Inspect the exact
private target receipt before performing the independently approved replacement.
No implicit current context or direct HTTP collector fallback is used.

The command performs these steps:

1. Revalidate the proposal, its digest, canonical profiles and clean tool source.
2. Verify candidate authority and consumer bytes. Copy the checked chart and host
   CLI into the private directory, verify their digests again and remove write
   permissions. Build the API bridge and chart inventory helper from the bound
   source, without inherited build overlays or cross-compilation settings.
3. Recheck source cleanliness before target access. Collect current provider
   inventory and bind the exact two-Node pool to the approved routes and runtime.
4. Run preflight and production transport probes, install the baseline profile
   and workload, then collect the fixed baseline and enabled windows. A failed
   measurement check stops before asking the operator to replace a machine.
5. Verify source-loss, agent-restart and collector-restart recovery.
6. Write `private/replacement-target.private.json` and wait for the approved
   operator action. Validate the replacement identity and pool, retarget only the
   owned kubelet host route, and require fresh source collection and image checks.
7. Run NetworkPolicy allow/deny/allow controls on the resulting pool. Execute the
   verified production CLI's strict doctor, Node explain and Node history paths.
   Only bounded check outcomes are retained, not their raw response payloads.
8. Recheck final live image identities and revalidate the original proposal and
   source. Assemble the record and remove
   UID-owned Kubernetes resources. Provider cleanup remains pending.

The registration wait is at most 1,800 seconds. The separate, unchanged
120-second application recovery budget starts when the new Node UID is first
observed, before readiness. It includes inventory validation, route update,
observer attachment and fresh collection. Provider provisioning time is not
reported as application recovery time. The [qualification contract](node-context-qualification.md)
defines the fixed measurement and recovery budgets.

Do not edit runtime files or change the proposal while a run is active. An
interruption attempts Kubernetes cleanup. A lost create response is treated as
uncertain ownership: the runner retains the parent namespace and private
conflict receipts instead of deleting an object without its original UID.
Resolve that uncertainty through the approved cleanup process before another run.

## Evidence and completion

Completed windows are retained separately, so a later failure does not erase
them. `failure.json` contains the failed stage, an error type, a fixed API/kubelet
reason when available, cleanup state and source-plan binding. Unknown reasons
remain `unclassified`; raw exceptions, responses, credentials and logs are omitted.
A failed replacement cannot become a successful result. No runner outcome grants
provider qualification.
The `private/` directory may contain Node names, machine identities, provider
resource identifiers and ownership receipts; it is not a public artefact bundle.

On successful collection, `evidence/qualification-observations.json` records
cleanup as pending. After verified Kubernetes cleanup,
`evidence/kubernetes-cleaned-observations.json` sets the workload/RBAC removal
checks while retaining pending provider cleanup. The pending evaluation therefore
still fails its cleanup gate and has `qualified: false`.

Exit 0 means the implemented measurement, recovery, network, production CLI and
Kubernetes cleanup steps completed. Exit 2 means validation or execution failed;
exit 130 means interruption. Inspect retained cleanup evidence after a failure.
None of these statuses asserts removal of provider infrastructure.

After independently confirming removal of every approved disposable provider
resource, use the [cleanup finaliser](node-context-provider-preparation.md#provider-records-and-cleanup-confirmation)
with its separately supplied attestation. That produces the final measured record
and evaluation. A separate [independent qualification review](node-context-qualification.md#evidence-and-review)
is still required before publishing an exact support row. The runner authors
neither attestation.
