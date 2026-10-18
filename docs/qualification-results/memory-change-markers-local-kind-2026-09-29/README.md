# Local workload-change qualification — 29 September 2026

The opt-in marker profile passed the 47 functional assertions in
[summary.json](summary.json), including cleanup. The runtime ran on 28 September
UTC using two kind Kubernetes 1.37.0 arm64 Nodes, LinuxKit 7.0.12 and containerd
2.3.4. These are our own local results; managed-provider qualification is separate.

## Candidate and sources

The candidate was built from `8db94e5b9da459e837fe09a7c966a140a78f16ac` plus the
marker implementation. [source.json](source.json) records production Go inputs,
module files and toolchain; [test-source.json](test-source.json) records the test
inputs used for native validation. The summary retains five Linux binary hashes
and local image IDs. These IDs are evidence identities, not registry references.

Host verification passed the full root race suite, vet, both Linux architecture
builds and chart contract checks. Native Linux arm64 module verification, race
tests and vet covered markers, history, incidents, Kubernetes acquisition, API,
collector, client, CLI, TUI and the controlled history fixtures. Decoder and
maximum-size tests exercised strict boundaries, provenance and capture limits.

## Runtime coverage

The actual chart and candidate binaries served authenticated requests. A controlled
HTTPS range fixture supplied synthetic memory values labelled with real fixture
identities. The checks covered:

- namespace, named-target, core-object and Node-only permissions;
- all seven supported owner families and exact object lifetimes;
- event filtering by UID and exclusion of free-form private event text;
- warm and in-flight revocation, with plain history still available;
- real rollout and rollback to the original ReplicaSet, without treating its old
  creation time as the rollback time;
- a real memory-limit increase from 64 to 96 MiB and the kubelet's resize report;
- a bounded application restart and its source timestamp;
- same-name replacement with separate memory lifetimes in capture comparison;
- 16 targets with 241 points each, rejection of a seventeenth before provider
  acquisition, and explicit truncation to 64 markers;
- timeout/cancellation, concurrent-request rejection and independent live reads;
- schema-6 redaction, mode 0600, replayed caveats, refusal to overwrite and refusal
  to infer continuity from separate capture-local alias sets;
- real terminals at 80×24, 120×36 and 160×48: verified final-page scrolling, source
  switching, refresh, plain-history restoration and exit;
- marker-only rollback, removing its endpoint, flag and source permissions while
  authorised plain history and live reads continued working.

The maximum live response completed in 2.73 seconds. This is a functional deadline
observation, not a throughput or tail-latency benchmark. The history release,
fixture namespaces, fixture Node binding and memory APIService were removed.
Existing trace services and the shared cluster remain for later tests.

## Repairs and limits

Host regressions exposed redundant acquisition at maximum owner counts, formatted
capture bytes consuming compact evidence budgets, and absent/negative legacy Event
counts becoming one occurrence. Fixes preserve limits, permissions and source
clocks. A full 16-Job CronJob source sequence passes with fresh selection and marker
checks before disclosure. Its 5.33-second host observation excludes remote provider
time and is not live CronJob performance qualification. Ordinary history retains
its existing object-read path.

Retained terminal-driver failures came from expecting an untruncated table name
and consuming buffered text before scrolling completed. The corrected driver
checks the full name in the panel, final-page counters and the restored short
plain-history report. All three sizes then passed without product edits.

Events remain partial reports, not a complete audit log or proof of causation.
Synthetic values do not establish external retention or cAdvisor label enrichment.
No managed-provider support, independent review or adoption result is claimed.
See the [source and permission contract](../../memory-change-markers.md).
