# Node incident capture and replay

Capture one Node through the authenticated Kubernetes API connection:

```sh
kubectl memlens capture --node worker-a --include-history -o node-before.json
kubectl memlens replay node-before.json
kubectl memlens capture --node worker-a --include-history -o node-after.json
kubectl memlens compare --before node-before.json --after node-after.json --node worker-a
```

Node captures use incident schema 4. Deep Pod schemas 1/2 and restricted schema 3
retain their meaning. `--node` cannot be combined with namespace/Pod selectors
or an older schema. Replay and comparison work without a kubeconfig or cluster
connection. They reject mixed evidence domains.

## What is saved

One bundle contains the Node source record, its typed analysis, capture time and
tool version. `--include-history` adds bounded Node-only history: at most eight
Node instances and 61 points per instance. Source measurement times, collector
receive times, report failures, retained last-good data and coverage loss remain
explicit. Missing measurements remain unreported, including measured-zero
distinctions. History points are never converted into invented Pod observations.

Every capture attempt reads the requested history and source record, then obtains
a new analysis with a fresh contributor authorisation check. A denied secondary
Pod decision produces Node-only evidence without contributor identities, counts,
coverage or charge. A failed request aborts capture. The source record and
analysis must agree on Node identity and measurements; a change during the read
requires a refresh. The whole collection attempt has a 30-second deadline.

Files are written atomically with mode `0600`. Existing files require `--force`;
failed reads or writes preserve the existing file. Standard output (`-o -`) is
staged before export so a rejected oversized bundle does not produce partial
JSON. The shared incident envelope is capped at 64 MiB, with a stricter 16 MiB
Node file limit and nested entity, string and array limits. The decoder rejects
unknown, duplicated or case-aliased keys, terminal controls and inconsistent
access/source metadata.

## Redaction

Default Node captures keep the selected Node name and replace raw Node UIDs with
domain-separated SHA-256 fingerprints. These permit same-instance comparison
and remain correlatable pseudonyms; they do not make a capture anonymous.
History generation identifiers receive the same treatment. Contributor UIDs
are removed, namespaces and Pod/workload names become bundle-local aliases, and
workload kinds become `Workload`. The live response is not mutated.

`--include-sensitive` retains the authorised raw Node and contributor identities.
It never bypasses authorisation. Neither form contains kubeconfigs, tokens,
credentials, container paths or raw kubelet Summary bodies. System-container
categories come from the fixed normalised source contract.

## Comparison boundaries

Comparison requires the same Node instance, boot and source provenance, capture
and source times that do not go backwards, and memory evidence that was fresh at evaluation. Node
replacement, a changed boot, stale data or unavailable memory prevents comparison.
Each row remains an independent measurement; usage, available memory, working set,
RSS and observed Pod charge are not stacked. Missing operands do not become zero.
Gap estimates are compared only when both carry the same accounting qualification.
Contributor aliases are not matched across captures.

Accounting rules and source limitations are documented in
[Node memory analysis](node-memory-analysis.md). A capture is recorded evidence,
not proof that a provider or accounting profile has been qualified.

Rollback keeps schemas 1/2/3 available. Keep a schema-4-capable binary to replay
Node captures; older readers must reject them rather than silently omit evidence.
