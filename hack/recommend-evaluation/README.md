# Recommendation evaluation

This offline gate exercises the production memory, volume, MemoryQoS and replica
interpreters against a versioned corpus. It does not change recommendations,
collect incidents, access Kubernetes or run a workload. Standard binaries do not
import it. Run from the repository root:

```sh
make check-recommendation-evaluation
go run ./hack/recommend-evaluation > evaluation.json
python3 hack/recommend-evaluation/compare_revision.py \
  --base EXACT_BASE_COMMIT --output NEW_ABSOLUTE_DIRECTORY
```

The comparison runs the **same current corpus and evaluator** against the base
revision and current production rules. It stages the base's public Git archive in
a temporary directory, copies the current evaluator there, and removes that
snapshot afterwards. The current checkout is unchanged. Both reports, diagnostics,
base/head commits and evaluator hashes are retained. Compilation failure is missing
evidence and fails the comparison. A non-passing base remains visible; only a
passing current gate can succeed. This deliberately does not compare two different
corpora's headline scores. CI retains the paired reports for 14 days.

## Taxonomy and labels

Corpus and taxonomy version 1 cover every ID in `internal/recommend`. The rule
inventory in `types.go` maps each ID to its evidence requirements. An AST-based
test requires new production IDs to enter this inventory. Every non-safety rule
needs positive and negative labels. The read-only guard needs positive coverage
and has a dedicated missing-guard regression.

Each case labels diagnosis, confidence, expected next-check IDs, prohibited IDs
and abstention. Labels are written before evaluating the cases. Input DTOs carry
only numbers, explicit missing values and closed state vocabularies. Adapters call
`AnalyzePodAt`, `AnalyzeVolumes`, `InterpretMemoryQoS`, `ForFinding`, `ForVolumes`,
`ForPodMemoryQoS` and `replicabaseline.Analyse`; they do not duplicate the rules.
All time-relative comparisons use a fixed clock. Scope identities required by the
domain APIs are fixed synthetic values constructed in the adapters.

Memory abstention means a normal/mixed diagnosis requiring bounded observation,
not a claim that the workload is healthy. Volume and QoS abstention means no
additional recommendation. Asking for fresh QoS evidence is an investigation, not
an abstention. Replica abstention means insufficient comparable peers; an ordinary
within-threshold result is a successful comparison. Replica results are
informational context and never become a sizing recommendation. Storage analysis
has no confidence score: its label is explicitly `not-assessed`.

## Metrics and gates

For each rule, expected/present is TP, unexpected/present FP, expected/absent FN
and unexpected/absent TN. Precision is TP/(TP+FP), recall TP/(TP+FN). A zero
denominator is `null`, never a perfect score. Evidence coverage is the share of
emitted known checks that match the case's evidence label and carry an action,
rationale and conditions. This measures labelled support within this corpus; it
is not an independent causal or clinical assessment.

Each case must exactly match diagnosis, confidence, expected checks and abstention.
Every rule needs coverage. No failing family may be hidden by an aggregate score.
Prohibited advice, unknown rule IDs and missing read-only guards have zero
tolerance. Changed action, priority, rationale or conditions also fail a separate
reviewed-copy hash. Hashes detect text changes; they do not understand or certify
natural-language safety. Current bounded read-only copy and its caveats were
inspected when the hashes were frozen. Do not regenerate them merely to pass CI.

Reports contain case IDs, source categories, rule/family counts and fixed result
fields. They exclude measurements, scope identities, receipt hashes and advice
text. Candidate advice with an unknown ID is counted without exporting that ID.
The corpus is capped at 1 MiB/256 cases; duplicate keys/case aliases, excessive
nesting, unknown fields, invalid numeric ranges and ambiguous PSI inputs fail.

## Fixture provenance and scope

The initial corpus contains 49 **synthetic** regression cases and seven numeric projections
from verified owned local-cluster runs. Its passing result
is not real-incident precision, independent review, managed-provider evidence or
adoption. It covers composition boundaries, OOM/pressure precedence, stale/future
termination times, cumulative versus recent counters, absent controls, adverse
and conflicting volume reports, tmpfs attribution and unsafe replica reference
sets. No user incident or external adopter record has been supplied.

The local projections retain actual anonymous-memory counters from the Go profiler
fixture and the five charge values from the controlled replica outlier. Source and
cleanup hashes bind retained private receipts. Synthetic scope identities and a
normalised clock make replay deterministic; the replica's missing change-history
confidence is preserved. These are numerical replays, not new runtime runs or
retests of authorisation. The original Go fixture's unlimited `memory.high` is not
projected as a finite control; this case tests memory diagnosis only. Additional bounded scenarios cover an 8 MiB file-cache charge, an 8 MiB tmpfs
charge, 61 recent `memory.high` events, one Kubernetes-confirmed `OOMKilled`
termination and tmpfs filesystem counters. Pressure was sampled while a read
stalled, then the fixture's original high boundary was restored so it could finish.
Two unsuccessful pressure attempts were retained and cleaned up before this run.
All test Pods used 64 MiB limits; their owned namespaces and cgroups were removed.
The OOM projection retains a measured pre-termination snapshot and the separate
recorded termination age; it does not invent post-termination counters. Volume
counters came from the owned mount's `df`, not a new kubelet or CSI validation.

A local-cluster case must use only owned fixtures, the numeric allow-list and a
SHA-256 receipt that binds the original run, authorised collection, sanitisation
and cleanup. A consented-incident case additionally requires recorded explicit
consent. The parser validates provenance structure; a maintainer must inspect the
actual receipt and input mapping before committing that label. No consent is
inferred from possession of data. Preserve source categories in every report.

## Initial aggregate result

The [frozen version 1 report](results-v1.json) records 56 passing cases, all 15 rule
IDs covered, and no false positives, false negatives, prohibited advice or missing
safety guards in this labelled corpus. The same corpus against the implementation
base and candidate produced no case changes. The report carries its corpus digest;
rerun the evaluator after a rule or fixture change rather than treating this
snapshot as a current or real-incident accuracy claim.

## Reviewing changes

1. Retain the failing report and identify FP, FN, confidence, abstention or copy
   changes. Inspect the numerical evidence and source limitations first.
2. Add a privacy-safe boundary/conflict case independently of the proposed code.
   Do not relabel a failure to match current output.
3. Run the same new corpus against base and candidate; retain both reports.
4. Review high-risk OOM, pressure, tmpfs and storage advice for unsupported
   remediation, attribution and loss of caveats. Review changed copy explicitly.
5. Change labels or copy hashes only with a documented evidence-based reason.
   Re-run the gate and local scenarios affected by the rule. A failing rule change
   is not merged; revert it without changing evidence collection.

External reviewer/adopter activities are outside this delivery. Their absence is
not counted as a passed review. These own tests cover the ticket's local scenarios;
they do not establish managed-provider qualification or accuracy on user incidents.
