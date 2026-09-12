# Node-context qualification contract

Node-context qualification is separate from the existing cgroup provider matrix.
The profiles and validators under `hack/node-qualification/` define the required
evidence and budgets. They do not claim that any provider run has taken place.
The owned kind runner supplies local measurements. The
[provider command](node-context-provider-execution.md) requires a separate,
explicitly approved run; provider rows cannot be inferred from kind.

## Fixed profiles

The canonical profiles cover pinned local kind 1.36.1 and 1.37.0 images, plus
GKE Standard, EKS managed Linux, AKS Linux and self-managed Linux reference runs.
Provider profiles require a current provider-owned inventory receipt. An old
cgroup qualification result is not evidence for the new kubelet path.

Each profile digest covers every setting except the digest field itself. Run
records bind that digest, production and qualification-tool commits, source tree,
image, chart, values, CLI, producer, serving trust and audience digests. A changed
budget or configuration requires a new digest and a fresh run; do not adjust
thresholds to turn an observed failure into a pass.

The local profile uses one owned kind Node. Provider profiles use two Linux Nodes
from one homogeneous, explicitly selected pool. The workload is 32 containers,
eight per Pod, using the repository's pinned BusyBox image. The workload remains
present throughout both measurement windows. This is a bounded qualification
load, not a claim about the maximum supported cluster size.

## Measurement protocol and budgets

Settle for 30 seconds before each phase. Measure a 120-second baseline with the
Node-context producer disabled, then 660 seconds with it enabled. Sample every
15 seconds, with at most 128 samples per phase. Each phase must span its full
duration without a gap exceeding two sample intervals. Missing observations are
null and fail their checks; they must not become zero.

| Measurement | Predeclared limit |
| --- | --- |
| Producer mean CPU | At most 50 millicores |
| Producer peak memory | At most 64 MiB |
| Kubelet mean CPU increase over baseline | At most 100 millicores |
| Kubelet peak memory increase over baseline | At most 64 MiB |
| Source acquisition p95 | At most 1 second |
| Raw Summary response | At most 4 MiB |
| Existing cgroup-agent scan p99 | Strictly below 4 seconds |
| Each required recovery | At most 120 seconds |
| Unexpected restarts, OOMs and steady source-failure increase | Zero |
| Workload mapping and Node coverage | Exact profile counts in every sample |

CPU samples represent CPU used during the observation interval, expressed in
millicores from cgroup v2 `cpu.stat` usage deltas. Producer and kubelet memory
means cgroup v2 `memory.current`, not RSS or working set. On multiple Nodes,
record the maximum component CPU/memory per sample
so a busy replica cannot disappear in a cluster average. The kubelet comparison
weights CPU means by each measured interval rather than treating unequal
intervals as equal. It
uses the same observation method and workload in both phases. Report this
controlled before/after result for its exact environment, not as a universal
causal estimate or provider-wide performance guarantee.

The producer's duration and response-size metrics describe its latest acquisition.
Count a gauge sample only when the successful acquisition counter advances.
Counter resets, missing counters or too few new acquisitions fail the result.
Nearest-rank percentiles are used. Intentional disruptions occur outside the
steady measurement windows.

## Lifecycle evidence

The producer's projected credential lifetime is 600 seconds. Observe a real
projected credential change while the producer identity stays unchanged, followed
by successful acquisition. Editing a credential file or restarting the producer
does not prove rotation.

Record source loss, retained stale evidence and recovery; cgroup-agent restart;
collector restart; and Node identity replacement. Each event requires measured
elapsed time, verified identity and fresh evidence after recovery. A local Node
object re-registration must be labelled as such. It does not qualify a provider's
machine-replacement procedure. The local fixture recreates the two source Pods
for the new Node UID and requires the collector's history generation to stay
unchanged. Provider runs separately require a real approved
provider replacement and independently checked cleanup.

## Evidence and review

Run the complete local fixture, including production preflight, authenticated
ingestion, CLI/TUI checks, the fixed measurement windows and lifecycle tests:

```sh
NODE_CONTEXT_ACKNOWLEDGE=create-and-remove-node-context-kind \
NODE_CONTEXT_VERIFY_INGESTION=true \
NODE_CONTEXT_QUALIFICATION_PROFILE=hack/node-qualification/profiles/kind-137.json \
NODE_CONTEXT_CLUSTER=kube-memlens-node-context-qualification \
NODE_CONTEXT_ARTIFACT_DIR=/absolute/path/to/new-evidence-directory \
hack/verify-node-context-kind.sh
```

For 1.36, select `kind-136.json` and set `NODE_CONTEXT_NODE_IMAGE` to that profile's
exact image. The runner refuses an existing cluster. It removes its own cluster
and locally built image after success or failure. It needs the same tools as the
normal kind verifier and read access to the owned Node's cgroup v2 counters.
Host observation uses `docker exec` and the Node's `crictl`, `nsenter`, `setpriv` and `curl`;
it adds no privileges or mounts to the production producer.

For a shorter recovery regression loop, replace
`NODE_CONTEXT_QUALIFICATION_PROFILE` with `NODE_CONTEXT_LIFECYCLE_PROFILE` using
the same local profile path. This runs the production preflight and UI checks
followed by lifecycle disruptions, and writes `lifecycle-check.json` with
`qualified: false`. It skips the fixed measurement windows and cannot supply a
qualification record. Do not set both profile variables in one invocation.

Run without competing builds or benchmarks on the Docker host. The harness does
not stop unrelated local workloads. Its measurements describe the observed host
conditions, including any competing load. It reads only the projected credential's
hash inside the Node and retains no credential or hash in the shared record.
The hash operation drops to the chart's UID/GID 65532 to follow the projected
symlink. It leaves `fs.protected_symlinks` enabled and does not add producer
capabilities or mount an extra volume into the producer.

`qualification-observations.json` is written before cleanup with cleanup still
pending. After verified removal, the runner writes `qualification.json` and
`qualification-evaluation.json`. Only the latter pair describes confirmed
cleanup. The pre-cleanup file is retained as an intermediate observation, and
does not qualify the profile. Each completed window is also retained separately
as `measurements-baseline.json` or `measurements-enabled.json`, bound to the
profile digest, so a later failure does not erase those measurements.
Failure or missing evidence never becomes a
passing support row. Local chart identity hashes the sorted chart tree, including
paths and contents; provider evidence must identify the immutable chart archive.

Evidence schema 1 contains exact environment fields, source availability,
explicit provenance, bounded samples, lifecycle outcomes, cleanup and privacy
assertions. The encoded file is limited to 512 KiB. Unknown or duplicate fields,
unbounded arrays, invalid timestamps and identifier/credential-bearing content
are rejected. Internal Node/Pod IDs, contexts, kubeconfigs, raw responses and raw
logs remain outside shared evidence. Evidence files are published atomically
without replacing existing files and use mode `0600`.

Evaluate a completed record:

```sh
python3 hack/node-qualification/evaluate.py \
  --profile hack/node-qualification/profiles/kind-137.json \
  --evidence evidence.json --output evaluation.json
```

Exit 0 means the supplied measurements passed their checks. Exit 1 means measured
or missing evidence failed a check. Exit 2 means the contract was malformed.
Every evaluator result remains `qualified: false` and `reviewState: pending`.
Unknown provenance and unreported optional fields remain explicit even when the
measurement checks pass. No accounting qualification is inferred.

Independent review requires a separate attestation binding the exact record and
profile digests, an approval decision and a review timestamp. It must be supplied
by the independent review process, not generated automatically by the measurement
runner. It is an operator attestation, not a cryptographic certification of the
measurements. Review must occur within seven days; qualification expires 30 days
after collection for the checked-in profiles. Dirty-source runs cannot be
promoted. Provider review also validates the current inventory receipt, canonical
inventory profile, environment tuple and qualification-tool commit.

```sh
python3 hack/node-qualification/review.py \
  --profile hack/node-qualification/profiles/gke-standard.json \
  --evidence evidence.json --provider-receipt provider-inventory.json \
  --attestation independent-review.json --output reviewed.json \
  --acknowledge reviewed-node-stats-evidence
```

Each provider run still needs explicit approval of its account/context,
immutable artefacts, trust/audience settings, network prerequisites, replacement
operation and cleanup plan. The qualification tooling must not provision cloud
resources, alter provider policy or obtain `nodes/proxy` as a fallback.

Use the [offline provider preparation tool](node-context-provider-preparation.md)
to validate local inputs and render a private proposal before requesting that
approval. Rendering a proposal does not execute or qualify a provider profile.
