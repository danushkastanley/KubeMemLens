# Changelog

All notable changes are documented here. KubeMemLens follows [Semantic Versioning](https://semver.org/).

## 1.0.6 - 2026-11-28

Patch release. Candidate and publication gates are recorded in the
[release preparation record](docs/release-preparation-1.0.6.md). It reads each
Pod's own cgroup under snapshot schema 8 and aliases the names in a default
incident capture. Read "Changed" before upgrading: some output that scripts may
read is different.

### Security

- Alias the names in a redacted incident capture. A default `kubectl memlens capture` (incident schemas 1 and 2) removed UIDs, container IDs, cgroup paths and labels, but kept the real namespace, Pod, node, container, owner and workload names while declaring `redacted: true`. Someone attaching a default capture to a public issue could publish names they believed were removed. Each name is now a capture-local alias such as `namespace-1/pod-1`, as in incident schemas 5 to 7, and the runtime class name and memory record names are removed. `--include-sensitive` is unchanged. Captures written by earlier versions still hold real names. Restricted incidents (schema 3) keep display names, as before.

### Changed

- A Pod's limit is the Pod cgroup's own `memory.max` when that is finite, measured against the Pod cgroup's `memory.current`. It was the sum of the container limits, which is wrong for a Pod that uses Kubernetes pod-level resources: a Pod limited to 96Mi with one 32Mi container read 128Mi. Without a finite Pod cgroup limit the container sum is used as before. For a Pod whose containers are all limited the limit is the same, and the percentage now includes the sandbox and can be slightly higher. Container, workload and namespace rows are unchanged.
- `explain -o json` writes explanation schema 6 for a document that carries Pod cgroup, zswap or `oom_group_kill` evidence; a document without them stays schema 3. With 1.0.6 agents nearly every Pod has a Pod cgroup, so a script that checks `schemaVersion == 3` needs to accept 6.
- A redacted capture is replayed and compared by alias, for example `--pod namespace-1/pod-1`. `capture` prints the alias to use, and `replay` and `compare` list the aliases when a selector does not match. Aliases are numbered separately in each file, so `compare` prints a caveat.
- The LIMIT cell of a Pod table can name a container and so contain a space, as in `288Mi (app 95%)`. Scripts should read `-o json` or `-o yaml`. In the TUI the LIMIT column is wider, other columns give up the space, and the gauge bar is left out of a cell that names a container.
- The TUI Pod table's DIAGNOSIS cell shows the Pod analysis, as `top pods` and the detail pane already did. A Pod with a recent OOM kill, node pressure or a container at its own limit now reads `oom-risk`, `memory-pressure` or `limit-risk` there.
- The agent publishes a cgroup v1 node with no containers and counts the scan as a success; it used to fail every scan, so the node showed only as missing. `doctor` fails its `cgroup mode` check for such a node and names the cause. See "Added".
- `status` reads the node list as well. A caller who cannot list nodes gets `cgroup version: not checked; node status could not be read` and the rest of the report as before.
- The collector's `/api/v1/namespaces` read takes the schema header and refuses an invalid one with `400`, like the other reads.

### Added

- Snapshot schema 8. The agent reads each Pod cgroup once per scan (`memory.max`, `memory.current`, and `oom`, `oom_kill` and `max` from `memory.events.local`) and keeps `zswap`, `zswapped` and `oom_group_kill`, which it parsed and dropped before. No chart value, RBAC rule or host mount changes. Agents, collectors and CLIs negotiate the schema, so a 1.0.5 component keeps working beside a 1.0.6 one; with a 1.0.5 agent or collector a Pod keeps the container-sum limit. [ADR 0032](docs/adr/0032-observe-the-pod-cgroup-under-snapshot-schema-8.md) records the decision and the mixed-version behaviour.
- Diagnosis from the Pod cgroup: `limit-risk` when Pod cgroup usage is near its own `memory.max`, and `oom-risk` when the Pod cgroup records an OOM at its own limit. Neither was visible from the containers alone.
- `explain pod` prints the limit source and the Pod cgroup's charge, limit and events. Zswap appears beside swap, and `oom_group_kill` beside `oom_kill`, only when the kernel reports them.
- `top pods -o json`: `limitSource` (`pod-cgroup` or `container-sum`), and `limitClosestContainer` with `limitClosestContainerPercent` when a container is closer to its own limit than the Pod figure shows.
- The container closest to its own limit, in the LIMIT cell of `top pods`, `explain workload`, incident replay tables and the TUI, and as `Closest container: app at 95% of its limit` in the TUI detail pane. A Pod at 11% of its summed limit can hold a container at 95% of its own.
- Metric series, each only when the value is known: `type="pod_cgroup_current"`, `"pod_cgroup_max"`, `"zswap"` and `"zswapped"`; `event="pod_cgroup_oom"`, `"pod_cgroup_oom_kill"`, `"pod_cgroup_max"`, `"oom_group_kill"` and `"local_oom_group_kill"`, with their `_delta` forms. The Pod `type="max"` series still means the container sum. See [metrics](docs/metrics.md).
- A cgroup v1 sentence in `doctor` and `status`: `1 of 2 reporting nodes use cgroup v1; KubeMemLens reads cgroup v2 only, so those nodes have no container memory evidence (see docs/compatibility.md)`. The agent logs the version once per node, not on every scan.
- A notice for an unknown TUI filter value, for example `Unknown filter severity:warn. Use info, medium, high or critical.` The token is not applied and the rest of the query still is. The README lists the accepted values.
- A `collector history` line in `doctor`. Until the collector has received 15 minutes of snapshots it names the start time and says that nothing from before then is kept; after that it reads `collector has run since <time>`. The line always passes. It is stage 1 of [ADR 0033](docs/adr/0033-collector-state-across-restarts.md).

### Fixed

- Stop applying a TUI filter value the interface does not know. `severity:warn` emptied the table, `pressure:high` showed the Pods without pressure, and `state:old` did nothing, each without a word.
- Match the TUI text filter against the diagnosis the row shows. Typing `oom-risk` missed a Pod flagged through a recent OOMKilled termination.

### Trace extension

The trace extension is built and installed separately and is in no release artefact.

- Rename `prototype/trace` to `extensions/trace`. The two module paths become `github.com/danushkastanley/kube-memlens/extensions/trace` and `.../extensions/trace/worker`. This is step 4 of [ADR 0031](docs/adr/0031-keep-the-trace-extension-as-a-separately-built-extension.md); the ADR also records that its test-trimming step was dropped.
- Build the local trace image again. `docker build -f extensions/trace/Dockerfile .` had failed since 1 October 2026 because the Dockerfile did not copy `internal/kubeprincipal`. A test now fails for any root-module package the build needs and the Dockerfile does not copy. The release image was not affected.
- Update `golang.org/x/sys` to 0.50.0, `golang.org/x/net` to 0.63.0 and `go.yaml.in/yaml/v2` to 2.4.5 in the trace modules.

### Dependencies

- No Go module, builder image, runtime image or chart dependency of the released artefacts changes.
- Update the pinned GitHub Action `github/codeql-action` to 4.39.4.
- Update `csi-node-driver-registrar` to 2.18.1 in the CSI fixture and the frozen volume qualification profile, and the `golang` and `busybox` digests in test fixtures.
- Refresh five package pins in the Linux terminal qualification image, which no longer built. Its Ubuntu 24.04 base and every terminal version in the compatibility claim are unchanged.

### Documentation

- Record the Artifact Hub listing, the Homebrew tap and the Scoop bucket in the README, the installation guide and the release process, and state that published artefacts are never replaced.
- [ADR 0033](docs/adr/0033-collector-state-across-restarts.md): how collector evidence should survive a collector restart and a Pod replacement, and what is implemented so far.
- Say what a default capture holds in the security model, the threat model and the tenant-isolation runbook.

## 1.0.5 - 2026-11-25

Patch release. Candidate and publication gates are recorded in the
[release preparation record](docs/release-preparation-1.0.5.md). It carries the
fixes from the November 2026 codebase audit. Read "Changed" before upgrading:
several defaults behave differently.

### Security

- Update Go to 1.27.4 across all three modules, pinned builders and trace executable validation to address GO-2026-6859 (CVE-2026-97054), an HTTP/1 server crash in `net/http` when a handler that uses `MaxBytesReader` reads the request body while writing response headers. Published release artefacts are unchanged.
- Keep a node reporting when one of its Pods has metadata beyond the collector's limits. A Pod the API server accepts, with more than 64 labels, an owner or workload name or kind over the limit, or more than 256 memory-backed `emptyDir` volumes, made the collector reject every snapshot from that Pod's node, so one workload could hide a node's memory evidence. The agent now fits such metadata inside the limits before posting, keeps the labels KubeMemLens reads first, never drops an unbounded volume from the count, and reports reduced Pods as `kubememlens_agent_metadata_reduced_pods`.
- Refuse control characters, Unicode format characters and invalid UTF-8 in every string an agent supplies. Names were checked for length only, although they reach metric labels, log lines and the terminal.
- Refuse terminal control sequences in deep incident bundles on replay.
- Replace terminal control, format and line-separator characters with U+FFFD in every response the CLI and TUI decode. The kind and name in a Pod's owner reference are not restricted by the Kubernetes API server and were printed as received, so a tenant who could create a Pod could send escape sequences to an operator's terminal and into CSV output.
- Offer a "next command" only when every name in it is a plain Kubernetes name. `explain` and the TUI built copyable commands from the workload kind and name, so a crafted owner reference could put shell text into a line the operator is invited to run.
- Stop one producer credential from holding every ingestion slot. A producer may have one snapshot in progress, and a snapshot body must arrive within five seconds. A holder of one agent token could previously trickle request bodies, keep all four slots busy and have every other node turned away.
- Build release archives from modules downloaded and verified in the build job. The release and candidate builds restored a Go cache shared with other workflows, and the candidate's second build reused the first build's caches, so the reproducibility comparison could not have detected a tampered cache entry. Published artefacts are unchanged.
- Give the certificate bootstrap an install mode, `extensionTLS.source=bootstrap`, in which the chart renders no private key. See "Added".

### Changed

- `kubectl memlens` commands without `-n` now use the namespace of the kubeconfig context, as `kubectl` does, and `default` only when the context names none. Scripts that relied on `default` need `-n default`.
- `MEMLENS_COLLECTOR_URL` on its own no longer switches the CLI to unauthenticated HTTP. It supplies the address only with `--connect-mode=http`; `--collector-url` is still an explicit choice. Plaintext use prints a warning, and a variable that is set but unused prints a note.
- The collector binary defaults to `--ingestion-mode=authenticated` and refuses to start without its serving certificate files. The unauthenticated mode needs `--ingestion-mode=legacy`. Chart installs already passed the flag and are unaffected.
- Continue tokens for paged reads have a new format. A token from before a collector restart, or from an earlier version, returns `410 Expired` where it used to return `400`; start the list again. A token used against a different view still returns `400`.
- The chart installs a weekly `kube-memlens-cert-rotation` CronJob with its own ServiceAccount, limited to the one serving Secret and the one APIService by name. Set `extensionTLS.rotation.enabled=false` to opt out. See "Added".
- The agent walks only the kubelet's part of the cgroup tree. Containers outside Kubernetes on the same node are no longer listed as unmapped, so the unmapped count and mapping coverage change on mixed-use nodes.
- The agent sets `GOMEMLIMIT` to 90% of `agent.resources.limits.memory`.
- History times are printed as dated UTC and labelled, where they were unlabelled local times.
- OpenMetrics counter families are declared without the `_total` suffix, as the format requires. Sample names are unchanged.
- Ctrl+C cancels a command, which exits with code 130 and prints nothing.
- The collector answers a second concurrent snapshot from the same producer with `429 producer_busy`, and a snapshot body that takes more than five seconds with `408 body_timeout`. Agents send one snapshot at a time and retry, so chart installs are unaffected.

### Added

- Scheduled serving-certificate rotation. The certificate was rotated only on install, upgrade and rollback, so a release left alone for a year lost the aggregated API. The weekly CronJob runs the same check; the collector reports the mounted certificate's expiry as `kubememlens_extension_serving_certificate_not_after_seconds`; the optional alert rules gain `KubeMemLensServingCertificateExpiring` with a runbook. [ADR 0030](docs/adr/0030-rotate-the-serving-certificate-on-a-schedule.md) records the decision.
- `extensionTLS.source=bootstrap`: a Job creates the serving Secret in the cluster, so two renders are identical and neither the manifests nor the Helm release Secret hold the private key. The default, `chart`, is unchanged and supports the Helm CLI only. This mode has been run on kind with the Helm CLI and with `helm template | kubectl apply`, not under Argo CD or Flux. [ADR 0028](docs/adr/0028-let-the-certificate-bootstrap-own-serving-tls.md).
- Agent chart values: `agent.priorityClassName`, `agent.updateStrategy`, `agent.podAnnotations`, `agent.affinity`, `image.pullSecrets`, and opt-in `agent.probes`. The probes need a listener on the Pod address that serves `/livez` and `/healthz` only; `/livez` fails when the scan loop stops.
- `collector.tokenExpirationSeconds`, separate from the agent's.
- `namespace.podSecurityEnforce`, to label a namespace the chart creates. The installation guide now states that the agent needs the `privileged` Pod Security level, because of its read-only `hostPath` volume.
- `--request-timeout` on every command, from `1s` to `5m`.
- `kubememlens_collector_agent_capture_skew_seconds`, the collector's receive time minus the agent's capture time per node, and `kubememlens_agent_snapshots_withheld_total`.
- The Krew index version bump is requested automatically when a stable release is published.
- A chart icon.

### Fixed

- Keep a node's memory evidence when `memory.pressure` cannot be read or parsed. Any read error other than a missing file failed the whole container, so on a kernel with PSI compiled in but disabled at boot, where the file exists and every read fails, the agent reported `scan_failed` for the node indefinitely. Pressure is now reported as unknown, as `io.pressure` already was, and the rest of the sample is kept. PSI averages that are not a percentage between 0 and 100, including `NaN`, are rejected in the parser; a `NaN` previously failed JSON encoding of the node's whole snapshot.
- Bound the container count of an agent snapshot before it is decoded. The 10,000-container limit was checked only after decoding, so a request within the 4 MiB body limit could list over a million empty containers and make the collector allocate several gigabytes, enough to have it OOM-killed and lose its in-memory history. This needed an agent ServiceAccount token, or network access to the ingestion port in legacy mode. Such requests are now rejected with the same `containers exceeds maximum` response while they are still bytes.
- Keep `metrics/current` scrapeable when per-entity series do not fit in the response. The entity ceiling was derived from the full response limit, but the exposition text is rendered within half of it, so at chart defaults a cluster of roughly 1,000 to 1,920 Pods got HTTP 507 for the whole resource and lost every series, including the collector state that the reliability alerts read. A level that does not fit is now left out on its own, most detailed first, and reported as `kubememlens_metrics_dropped_entities{reason="response_budget_exceeded"}`. Output is unchanged when everything fits.
- Flag a Pod when one of its containers is near its own memory limit, and rank it accordingly.
- Skip cgroups that stop during the agent's walk; a container that exits mid-scan no longer makes the scan partial.
- Keep Kubernetes lookups out of the agent's scan timeout, and retry a failed Node lookup after 5 seconds, not the full cache lifetime.
- Treat the sandbox of an idle Pod as infrastructure, and identify a container below its Pod cgroup on nested runtimes.
- Stop reporting an unreported kernel total as zero, and tell an unlimited memory limit from an unknown one in `top`.
- Read deep captures as of their capture time on replay.
- Fail `doctor` when an expected node has never reported, and name the checks that fail.
- Stop reporting an empty namespace as a rebuilding collector.
- Name kubeconfig and context problems; they were hidden behind a generic message.
- Stop copying every container record on each collector read, release the read gate before writing the response, and serve health probes outside the read concurrency limit.
- Charge the ingestion rate limit after admission and per producer.
- Report an owner, workload or termination reason that holds control characters as absent, and count the Pod as reduced.
- Refuse a chart install into a namespace that does not exist, and refuse four value combinations that break at run time.
- Render the agent's RBAC only when the agent is enabled.
- Make Ctrl+C and `q` quit from every TUI screen.
- Keep deep `top --watch` running through a collector outage.
- Accept `status -o` and `--sort-by anon`, and head `explain` tables `ANON`.
- Date a stale or degraded transition by the expiry that caused it.
- End a history read before the server's request timeout.
- Refuse a snapshot that repeats the stored capture time with different content; it was reported as accepted and discarded.
- Render each Pod and container metric series once when two instances share every label.
- Label the event source and unknown PSI truthfully in `explain` and `compare`.
- Bound cgroup file reads at 256 KiB and read each cgroup through its own directory.
- Bound the legacy direct-HTTP and Service-proxy readers at 16 MiB.
- Publish incident files on filesystems without hard links, still without replacing an existing file, and refuse a full session export to a terminal.
- Let `go build ./...` link on hosts that are not Linux.
- Correct stale release, history and eBPF claims in the roadmap and README.

### Performance

- Cache the TUI's filtered and sorted Pod list and score each Pod once per sort. One keypress plus a render at 5,000 Pods fell from about 169 ms to under 1 ms in the benchmark.

### Trace extension

The trace extension is built and installed separately and is in no release artefact. [ADR 0031](docs/adr/0031-keep-the-trace-extension-as-a-separately-built-extension.md) records the decision to keep it that way.

- `kubectl memlens trace --help` says the extension is separately built.
- Keep the final counts of a trace whose target is active at expiry; the worker stops forwarding 20 ms before the deadline.
- Tolerate one second of clock skew between the trace controller and a node. [ADR 0029](docs/adr/0029-tolerate-bounded-clock-skew-between-trace-controller-and-node.md).
- Keep a bind request's replay record until the request expires; it could be replayed after a short trace.
- Skip file records whose path is not valid UTF-8, reject bounds that cannot run, and report an exhausted output budget as `output_limit`.
- Give each principal its own admission request budget, so one caller cannot stop another from cancelling a trace.

### Dependencies

- Update the Go builder image digest, `charm.land/bubbletea/v2` to 2.1.2 and `github.com/charmbracelet/x/ansi` to 0.11.10, and OpenTelemetry to 1.47.0 across all three modules.
- Update the pinned GitHub Actions `ossf/scorecard-action` to 2.6.0 and `actions/setup-go` to 7.1.0.
- Dependabot now also covers the trace prototype Go module and Dockerfiles in nine more directories, and a weekly workflow reports image digests pinned in workflows, scripts and chart templates whose tag has moved.

### Documentation

- State that the default chart mode supports the Helm CLI only.
- Make Krew the first CLI install path.
- Add diff-driven verification and review guidance for contributors.

## 1.0.4 - 2026-11-10

Patch release. Candidate and publication gates are recorded in the
[release preparation record](docs/release-preparation-1.0.4.md).

### Fixed

- Make `metrics/current` scrapeable. When a request's `Accept` header prefers OpenMetrics or Prometheus text, the resource now returns the exposition text with the matching content type; other requests, including `kubectl get --raw` and the CLI, keep the JSON object. Since authenticated reads replaced the direct collector route, Prometheus and similar scrapers had received JSON they could not parse, so the chart's optional alerts and dashboard had no data. [Scrape with Prometheus](docs/metrics.md#scrape-with-prometheus) documents a least-privilege setup.
- Point the default alert `runbookBaseURL` at `danushkastanley/KubeMemLens`. The previous `kube-memlens` repository URL returned 404 for every alert's runbook link.

### Changed

- Remove command examples from the Krew manifest caveats, which Krew guidelines reserve for setup prerequisites. The caveats still name the Helm chart and metrics-server requirements.
- Keep release-candidate install paths out of the README, installation guide and chart README. Release archives and the chart package ship these guides and stable promotion preserves their bytes, so the v1.0.3 artefacts still name `v1.0.3-rc.1`. Candidate prerelease notes now carry the generated install command, and the release contract rejects candidate paths in the guides.

### Dependencies

- Update the pinned GitHub Actions: `docker/login-action` 4.8.0, `sigstore/cosign-installer` 4.1.3 and `github/codeql-action` 4.39.3. No Go module, builder image or runtime dependency changes.

### Documentation

- Publish the post-1.0 roadmap in `docs/roadmap.md` and summarise it in the README.
- Add a terminal demo GIF to the README.
- Add "Scrape with Prometheus" to `docs/metrics.md`.

## 1.0.3 - 2026-11-08

Patch release. Candidate and publication gates are recorded in the
[release preparation record](docs/release-preparation-1.0.3.md).

### Security

- Update `golang.org/x/net` to 0.62.0 in the standard and trace modules for CVE-2026-78661, a denial of service in `x/net/html`. govulncheck reports the affected code as unreachable from KubeMemLens; the update clears the image scan. This pulls the matching `x/crypto` 0.58.0, `x/sync` 0.24.0, `x/term` 0.47.0 and `x/text` 0.43.0 releases.
- Raise the indirect `github.com/klauspost/compress` dependency to 1.20.3 in all three modules for GO-2026-6865, a panic on two-block frames. govulncheck does not report the affected code as called.

### Dependencies

- Update the Kubernetes libraries to 0.37.2 across the standard, trace and worker modules, and bubbletea to 2.1.1.
- Pin the release builder to the 5 November 2026 `golang:1.27.3-alpine` rebuild (Alpine 3.24.4). The Go version is unchanged.

### Changed

- Build the sample cgroup data into the CLI, so `kubectl memlens sample` works from a release binary without a source checkout. `KUBEMEMLENS_SAMPLE_ROOT` still selects a directory of samples.
- Report `rss-heavy` only when anonymous memory is material: at least half of a finite limit, or 512 MiB without one, as well as 65% of the charge. Small anon-dominant cgroups, such as idle control-plane Pods, are now `normal`. The diagnosis name, metrics label and JSON value are unchanged.
- Label the anonymous memory bucket "Anonymous (anon)" and `ANON` in the CLI and TUI instead of "RSS / anon" and `RSS`, because cgroup `anon` is not process RSS. JSON, YAML and CSV field names are unchanged.
- Show `kubectl memlens` in usage text, and hide the pre-v1 rollback flags (`--connect-mode`, `--collector-url`, `--collector-namespace`, `--collector-service`, `--collector-port`) from help. They still work.

### First-run experience

- Say when the KubeMemLens API is not installed, with a link to the installation guide, instead of reporting a missing object.
- In restricted mode, note on stderr when the chart or metrics-server is missing, for table and text output only.
- Print next steps after `helm install`: wait for rollout, bind the namespace viewer role, then run `doctor` and `tui`.
- Extract `LICENSE` explicitly in the Krew manifest, and describe the chart and metrics-server requirements in its caveats.

### Documentation

- Rewrite the README opening with the problem statement, a three-step quick start, a comparison with `kubectl top`, k9s and Prometheus/Grafana, an explanation of how memory is measured, measured resource overhead, the exact permissions the chart installs, and download verification steps.
- State one provider support scope in the compatibility contract. GKE and self-managed results from `v0.0.1-alpha.3` are now marked historical rather than qualified, and internal ticket identifiers are removed.
- Add security@kubememlens.com as the private reporting address for vulnerabilities and conduct concerns, and put vulnerability reporting first in the security policy.

## 1.0.2 - 2026-10-24

Patch release. Candidate and publication gates are recorded in the
[release preparation record](docs/release-preparation-1.0.2.md).

### UI updates

- Open a dedicated Pod dashboard with Enter, showing memory composition, configured limit and headroom, Kubernetes context, retained history, container contributions and sampled event/pressure evidence.
- Return with Esc while preserving the parent filter, sort, selected Pod and scroll position, including when a refresh has reordered the table.
- Navigate panels with Tab / Shift+Tab and retain access to read-only checks, comparison, container detail and raw evidence. Compact terminals show one panel at a time; focus remains visible without colour.
- Keep the main dashboard layout unchanged. Display stale, partial, disconnected and unavailable evidence explicitly; plot observed history samples without interpolating gaps or combining Pod instances.

### Security

- Update Go to 1.27.3 across all three modules, pinned builders and trace executable validation to address GO-2026-6809 in certificate verification. Published release artefacts are unchanged.

### Dependencies

- Align `golang.org/x/sys` on 0.49.0 across the standard, trace and worker modules.
- Refresh the immutable CodeQL 4.39.0, Scorecard 2.5.0, Download Artifact 8.0.2 and GoReleaser action 7.3.0 pins.

### Release verification

- Preflight the pinned Helm test image before clean-consumer checks, using the existing identical-digest mirror only for classified registry rate limits. Preserve the connection-test timeout and report bounded hook state diagnostics on failure.

### Upgrade notes

- Pod detail now opens as a dashboard. Press `e` inside it for the complete line-oriented evidence, and Esc to return.
- No API, capture-format, permission, host-mount or persistent-data migration is introduced. The optional trace extension remains a separate development profile.
- Helm upgrade or rollback restarts the collector and resets its in-memory history. Rolling back to v1.0.1 restores the older Go toolchain; it is not a security remediation.

## 1.0.1 - 2026-10-19

Patch release. Publication and signed artefact status are recorded
in the [release preparation record](docs/release-preparation-1.0.1.md).

### Dependencies

- Align Kubernetes client and API libraries on 0.37.1 across the standard, trace and worker modules.
- Update Bubble Tea to 2.1.0 and align `golang.org/x/time` on 0.16.0 across all three modules.
- Refresh the pinned Docker, artefact-upload, Syft installer and CodeQL actions.

### Fixes

- Preserve source sample time, freshness and completeness in deep-mode `top` table, JSON, YAML and CSV output.
- Derive offline Pod comparison rates from forward source timestamps only when Pod identity and selection match and neither observation is stale. Omit unproven rates for redacted Pod captures and aggregate workload captures.
- Measure volume qualification recovery durations with monotonic sub-second precision, avoiding zero-second records for fast recovery.
- Wait for authenticated aggregation readiness after volume-profile rollback before asserting that the removed route returns 404, and retain redacted probe error categories.
- Delete owned qualification Pods before their Jobs so background garbage collection cannot race the explicit Pod cleanup; retain UID-bound deletion.

### Security

- Update Go builders and trace executable validation to 1.27.2, and align `golang.org/x/net` on 0.60.0 across all three modules to address HTTP and HTTP/2 security advisories.
- Update the optional trace worker's Moby user-database parser to 0.4.1. The separate trace extension retains its development status.
- Update CEL to 0.30.0 and the etcd clients/API to 3.7.1; migrate the optional worker SDK's containerd dependency to patched v2.3.7, retaining its tests and dependency isolation. The SDK patch has a new signed engine identity and requires a newly matched optional engine bundle.

### Upgrade notes

- `top` CSV output appends `captured_at`, `freshness` and `completeness`; JSON and YAML include the same evidence metadata. Consumers should read fields by name rather than assume a fixed column count.
- Offline comparison output omits the rate column when continuity cannot be established. Existing capture formats remain readable.
- The chart keeps the existing permissions, defaults, data retention and support scope. Helm upgrade or rollback restarts the in-memory collector and resets its retained history.

## 1.0.0 - 2026-10-06

Read-only memory inspection, incident capture/replay, access isolation, recovery
and cleanup have been exercised on AWS EKS. Install KubeMemLens and
[send feedback through a GitHub issue](https://github.com/danushkastanley/KubeMemLens/issues/new/choose).


### Fixes

- Permit the exact amd64 Go runtime thread flags in the optional trace node seccomp profile, preserving restrictions on namespace creation and worker processes.
- Recognise the Linux IMA module in optional trace preflight without accepting unknown security modules.
- Preserve current volume measurements when another kubelet filesystem sample has expired; count expired source records as omissions without changing their timestamps or ingestion limits.
- Match provider qualification to the rendered Service port and use controller-owned, bounded network-policy probes on strict CNIs.
- Give agent and Helm-hook Pods explicit ingress policies for strict CNI startup, while preserving namespace egress restrictions. Run the connection probe as a bounded Job so its Pod has controller ownership.
- Retain failed certificate-hook diagnostics and annotate hook resources with their Helm release identity for verified cleanup after interrupted installation.
- Avoid floating-point rounding in agent scan-duration metrics, keeping them consistent with the recorded scan timings.
- Report a container once when it creates its own sub-cgroups (systemd, Docker-in-Docker). Nested cgroups were listed as extra entries with the same container ID and their memory was counted twice.

### Optional trace operations

- Add an operator walkthrough, incident playbooks and diagnostic-sharing guidance for the separate development extension. Document expiry and cancellation outcomes without equating a missing admission with confirmed cleanup.
- Exercise the documented CLI, bounded redacted reports, metadata-only audit policy and installation removal locally. Resource and managed-provider qualification remain open.

### Optional incident timelines

- Add explicitly created, owner-only incident sessions with bounded in-memory retention, Pod captures, comparisons, change markers, private annotations and visible evidence gaps through the CLI and TUI.
- Reference local trace reports by exact-byte digest and typed outcomes without uploading report text. Keep operator-supplied references distinct from verified measurements; offline verification proves file agreement only.
- Separate sanitised and authorised exports, preserve schema-1 timelines and use schema 2 for trace references. Enforce current namespace and operation permissions; sessions expire after one hour and disappear on collector restart.

### Optional trace compatibility

- Negotiate an explicit trace contract before admission and guard activation against extension downgrades. Previous development clients retain their existing response shapes and server-enforced limits; incompatible current clients fail before tracing starts.
- Read bounded trace exports in schemas 1 and 2 without rewriting retained files. New schema-2 exports distinguish acknowledged and unknown contract versions; imported evidence remains untrusted.
- Verify previous/current development binaries locally and document upgrade order and deprecation windows. Resource, provider and release qualification remain incomplete.

### Optional trace audit and retention

- Add bounded, versioned audit records with keyed actor, tenant, session and immutable target references, frozen quotas and separate terminal/cleanup outcomes. Trace payloads remain outside routine audit logs.
- Require an administrator-owned audit key and exact digest for optional trace installations. Fail closed on uncertain audit delivery while preserving physical cleanup.
- Document zero server result retention and verify local quota rejection, expiry, cancellation, path redaction and kernel cleanup. Resource and provider qualification remain incomplete.

### Optional trace development packaging

- Add a separate, disabled-by-default trace chart with pinned images, policy bytes, TLS peers and node profiles. Keep standard installations unchanged and require explicit acknowledgement of unqualified development use.
- Validate installation trust and host prerequisites before workloads start. Reject node profile drift during admission and active revalidation; document administrator-owned prerequisites and verified removal.
- Exercise local Helm installation, upgrade, rollback, tenant/network isolation and active kernel teardown. Resource and provider qualification remain incomplete; no supported trace profile is declared.

### Optional worker attachment scope

- Restrict the constrained SDK to the tracepoint and fentry/fexit classes already permitted by signed file/cache/OOM objects, rejecting other classes before map loading.
- Remove unused discovery and network/TC/uprobe backend dependencies from the worker, with both-architecture dependency checks and non-loading object-preparation regressions. Preserve sealed execution and all resource gates; a new matched candidate still needs kernel qualification.

### Optional NUMA and HugeTLB context

- Add bounded read-only NUMA and HugeTLB sources, a named Node topology endpoint and a separate section in Node detail. Keep ordinary memory totals and Pod views unchanged.
- Keep pool total, outstanding reservations, current use, cgroup byte limits and cumulative allocation-limit failures separate. Missing topology never establishes balance or pressure.
- Add explicit schema-7 capture with source clocks, caveats and default Node/domain aliases. Keep ordinary Node captures at schema 4 and require a separate, unbound read role for the opt-in profile.

### Language-profiler handoffs

- Add opt-in recommendation schema 4 and terminal guidance for explicitly declared Go heap-profile workflows. Commands analyse existing local profiles and are never executed by KubeMemLens.
- Require fresh, complete container evidence; abstain on unknown declarations, sidecars, stale data and unsupported diagnoses. Public captures continue to strip profiler metadata.

### Replica memory baselines

- Add opt-in Pod and workload comparisons against four other authorised replicas with matching revision, immutable images and memory resource layout.
- Show robust peer distributions, outlier magnitude, confidence and explicit exclusions through `replicas` commands and the TUI's B panel. Current charge, available composition, retained growth, limit use, PSI and known local event rates remain separate evidence.
- Recheck named permissions and current membership before disclosure. Keep the profile disabled by default, with namespace source roles, an unbound viewer role, bounded local retention and no remote provider requirement.

### Workload change markers

- Add opt-in Pod, container and workload history context with exact instance/owner identity, bounded structured events, source timestamps and explicit retention and clock caveats.
- Add CLI/TUI marker selection and schema-6 capture, replay and comparison. Default captures use local aliases; comparisons never infer identity continuity across alias sets or splice memory across Pod UIDs.
- Keep existing history responses and viewer roles unchanged. Optional namespace event/owner roles, named context permissions and disclosure-time rechecks govern the additional reads.

### Optional memory trends

- Add named Pod, container, workload and Node history reads from a fixed, verified-TLS Prometheus source, with immutable instance labels, bounded queries and explicit source, freshness and gap semantics.
- Add CLI and TUI source selection with cancellation and explicit local fallback. Provider waits use a separate read gate; existing local history and incident formats remain unchanged.
- Keep the chart profile disabled by default, with separate acquisition and unbound viewer roles. Verify local installation, reader isolation, revocation, bounded queries, terminal source switching and rollback. Managed-provider qualification remains separate.

### Optional dependency notices

- Retain notices from compiled package directories and their module ancestors, including vendored forks. Keep their relative paths, exclude Go source files from the notice inventory and reject package directories outside their module.

### Optional admission idle cost

- Suspend expiry timers while the optional API and node service hold no state. Resume the existing 100 ms cadence for reservations, leases and replay records, including failed-cleanup retries. Resource qualification remains pending.

### Optional worker startup

- Remove unused socket-probe initialisation from the constrained SDK and strip worker debug sections from reproducible builds. Bind the updated SDK patch into signed acceptance and retain executable sealing and attachment/cleanup checks. Fresh kernel and resource qualification remains required.

### Dependency security

- Update OpenTelemetry to v1.45.0 for the exporter-configuration logging advisory, with its required transitive dependencies.
- Update the optional worker's containerd library to v1.7.36 for the ExecSync and image-pull exhaustion advisories. This does not update a cluster's container runtime; other recorded scanner findings remain unresolved.

### Local CSI qualification

- Add a frozen local volume qualification profile with measured Go heap, retention, CPU, API activity, source/delivery and operator latency.
- Verify source/build identities, controlled filesystem/inode pressure, source-specific health, scoped CLI/TUI workflows, recovery and cleanup. Keep managed CSI providers unqualified.
- Expose fixed aggregate retention and heap metrics through the existing authenticated endpoint, and delivery counters on the optional Node producer. No new permissions or volume identity labels are added.

### Volume correlation

- Add caller-authorised Pod and workload volume views, with filesystem bytes/inodes, CSI health and tmpfs configuration separate from memory totals.
- Correlate container I/O pressure, cache movement and dirty/writeback evidence without claiming causality or per-volume charge. Preserve missing values, source ages and old snapshot schemas.
- Add fresh volume-aware recommendations, comparison and schema-5 incident capture/replay. Default captures redact identities; linked comparisons require verified references and explicit sensitive exports.
- Add a separate opt-in workload profile with live controller ownership, bounded queries, shared-PVC deduplication and unchanged existing viewer roles. Managed CSI qualification remains separate.

### Node commands and incidents

- Add Node explain/history commands with source ages, missing-field states, qualified estimates and bounded contributor rankings.
- Add selected-Node TUI detail and wide panes, contributor ranking, cancellable polling, retained history and freshly authorised capture actions.
- Add redacted schema-4 Node capture, offline replay and compatible same-instance comparison, with fresh export authorisation and strict file/identity bounds. Preserve deep Pod schemas 1/2 and restricted schema 3.

### Node memory analysis

- Add a pure Node analysis model with source-labelled severity, bounded Pod/workload rankings, checked accounting and explicit unavailable estimates.
- Require current Node identity, matching source windows, complete cgroup coverage and operator-owned accounting qualification before showing a gap. Keep hugepages separate from ordinary memory values.
- Add the optional Node analysis API with a separate uncached Pod authorisation check; Node-only readers receive no contributor identities or counts.

### Optional Node-context collection and history

- Add a default-off producer for bounded, verified-TLS kubelet Summary reads, with its own ServiceAccount and no host mounts or Linux capabilities.
- Add authenticated snapshot schema 3, independent producer ownership, current Node UID binding and bounded Node-only reads/history. Preserve schemas 1/2 and existing viewer permissions.
- Retain source clocks and last good evidence through collection failures; report rebuilding, freshness and capacity in debug/doctor and aggregate operational metrics.
- Provide an explicit Helm profile and local upgrade, rollback, restart and reader-isolation verification. Managed-provider qualification remains separate; Node analysis and the incident cockpit are not included in this slice.

### Restricted incidents and qualification tooling

- Add incident schema 3 for restricted capture, deterministic offline replay and working-set comparison in the CLI and TUI. Retain source, sample windows, partial coverage and identity caveats; reject cgroup comparisons and schema downgrades.
- Preserve deep schemas 1/2. Enforce private, bounded output, atomic no-overwrite publication and explicit replacement without removing the previous capture first.
- Add a read-only restricted qualification harness with separate local, Autopilot, Fargate and virtual-node profiles. Managed-provider support remains unqualified pending approved runs and review.

### Restricted CLI and TUI workflows

- Add source-labelled working-set views for top, explanation, recommendation and TUI navigation, with explicit unavailable cgroup diagnostics and no automatic resource changes.
- Keep missing values distinct from measured zero, retain transient frames with source age, and clear data on permission or authentication loss. Preserve get-only deep Pod access and existing deep output.
- Add restricted explanation schema 4 and recommendation schema 2. Verify CLI and PTY workflows with local namespace credentials, including revocation/recovery and terminal restoration.

### Agentless current reader

- Add scoped Pod, owner, Node and Metrics API reads behind the common current-observation query, without installing KubeMemLens workloads.
- Preserve pending and missing-metric rows, source times, identity caveats and coverage. Working set remains separate from cgroup charge and composition.
- Bound request, response, object and output sizes; reauthorise each refresh and retain resource rows when optional sources fail. Existing deep readers and incident formats are unchanged.
- Verify measured, missing and revoked evidence with a controlled Metrics API on a collector-free local kind cluster.

### Optional CSI volume-health source

- Add caller-authorised, Pod-scoped CSI health reads with separate Pod, PVC and node-backend observations, bounded requests and aggregate-only exports.
- Keep missing alpha data unreported and unknown conditions adverse. Existing memory collection, permissions and persisted schemas are unchanged.
- Verify alpha-off, degradation, recovery and namespace isolation on disposable local Kubernetes 1.37 clusters with a pinned upstream CSI driver.

### Optional Metrics API source

- Add caller-authorised, namespace-scoped resource-metrics discovery and bounded reads, preferring served v1 with a discovery-driven v1beta1 transition path.
- Preserve working-set memory, CPU nanocores, source times and identities; distinguish missing, denied, partial, stale and unavailable data. Existing cgroup paths do not depend on this source.
- Verify both API versions and namespace denial through a local TLS fixture. Correct CI path classification so source and fixture directory changes trigger kind checks.

### MemoryQoS diagnostics

- Interpret observed reclaim protection, throttle and hard-limit controls, with separate freshness, resize, resource-correlation and pressure evidence.
- Keep unlimited leaf throttling distinct from unavailable controls and unobserved parent policy. Cumulative high counts no longer imply recent pressure.
- Show the shared interpretation in explanations, doctor, comparisons, replay and TUI detail, with read-only investigation recommendations. Explanation schema 3 adds the derived observations; snapshot and incident formats are unchanged.

### Pod memory resources and resize

- Show effective Pod memory budgets alongside container contributions, kubelet allocated/applied values and distinct resize states, without changing cgroup accounting.
- Refresh resource context through the existing informer and preserve it in CLI/TUI details, machine explanations, comparisons and redacted captures.
- Negotiate snapshot schemas for mixed-version readers and agents. Capture supports an explicit schema-1 export with an omitted-context caveat for older replay binaries.

### Kubernetes 1.37 client compatibility

- Align the Kubernetes client, API server and supporting modules on 0.37.0 while retaining the newer security-patched dependencies already selected for RC2.
- Adapt the extension authoriser to the 1.37 interface. Both authorisation entry points retain agent identity validation, one delegated access review and bounded audit output; unsupported condition evaluation fails closed.
- Exercise older 1.35/1.36 Pod response shapes through the production client and mapper, preserving resource availability when Pod budgets, allocated resources and volume-health fields are absent.

### Included from v1.0.0-rc.2

- Give the Helm connection-test hook a tested 16 MiB startup allowance instead of 8 MiB, which could OOM-kill container initialisation before the connectivity test ran. Keep its security settings, retry policy and application resource limits unchanged.
- Repair the unavailable Skopeo container pin using upstream's digest-pinned immutable tag. Check tool availability/version before release builds and registry operations, and distinguish a tool-pull failure from a missing destination image.
- Cancel superseded Pod history requests and isolate dependency diagnostics from the terminal renderer.
- Restore complete dashboard borders and height, distinguish namespace context from keyboard focus, and set a fixed terminal title without cluster identifiers.
- Update gRPC to 1.83.2 for its HTTP/2 fragmented-frame memory-exhaustion and missing-authority denial-of-service advisories, and golang.org/x/time to 0.15.0.
- Align Go and its pinned container builder on 1.27.1, all CodeQL actions on 4.37.9, and the Syft installer action on 0.24.2 while retaining Syft 1.49.0.
- Group coordinated Kubernetes and CodeQL Dependabot updates. RC2 preparation kept Kubernetes libraries on 0.36.4 pending the separate authoriser-interface migration recorded above.
- Freeze the provider-review CLI test clock to its evidence fixtures without weakening production evidence-age validation.
- Align every release SBOM validator with the installed Syft version and document the unpublished RC2 repository destinations.
- Update the etcd API/client modules to 3.6.14 for bounded TLS handshakes and x/crypto to 0.56.0 for SSH denial-of-service fixes, following the whole-dependency release-gate review.

RC2 was published on 9 September 2026 with prospective 1.0.0 chart and archive metadata. Its [preparation record](docs/release-preparation-rc2.md) preserves the original decisions and evidence.

## 1.0.0-rc.1 - 2026-08-29

### Added

- Prepared the repository to build prospective `v1.0.0` artefacts under the approved `v1.0.0-rc.1` candidate identity for no-rebuild stable promotion after adopter evidence and go/no-go evidence pass.
- Published immutable prerelease `v1.0.0-rc.1` with six CLI archives, SBOMs, signed checksums, provenance, exact candidate image/chart digests and a passing clean-consumer lifecycle. Stable `v1.0.0` remains unapproved and unpublished.
- Retained the two unpublished candidate workflow failures in release evidence issue #50 before resetting the public candidate identity to `v1.0.0-rc.1`.
- Added bounded native Go fuzzing for cgroup parsing, tenant-scoped pagination and snapshot decoding, with a dedicated pull-request and `main` CI job.

### Changed

- Defined one canonical v1 support and release contract covering exact target profiles, evidence owners, multi-tenant security, best-effort availability, bounded history, metadata exposure, unsupported environments and deprecation rules.
- Accepted a Kubernetes aggregated-API design for authenticated operator reads, tenant-scoped authorisation, node-bound agent writes and replay protection.
- Moved agent writes to `memory.kubememlens.io/v1alpha1` with Pod-bound identity, node binding, collector epochs, idempotent sequences, bounded abuse controls and serving-certificate rotation.
- Moved production CLI, TUI and metrics reads to tenant-scoped aggregated resources; the secure Service now exposes only TLS port `443` and keeps port `8080` health-only inside the collector Pod.
- Removed legacy direct ingestion, collector reads and ServiceMonitor rendering from the production Helm chart while retaining binary/client rollback code for pre-v1 installations.
- Narrowed the intended self-managed containerd qualification row to amd64 so one reviewed provider record cannot be misrepresented as arm64 evidence.
- Published one-time, digest-bound provider/runtime evidence for the exact GKE Standard, EKS managed-node, self-managed containerd and CRI-O combinations, with freshness reported as advisory rather than a scheduled or per-release cloud gate.
- Classified standard AKS as unsupported for the recorded candidate because AKS supplied no request-header proxy client-name constraint and the candidate correctly failed closed; GKE Autopilot, EKS Fargate, AKS virtual nodes, Windows nodes and cgroup v1 remain explicit unsupported rows.
- Qualified xterm, Kitty, Alacritty, tmux and delayed SSH for the exact terminal candidate, with 30-minute PTY and real-emulator runs; named macOS emulators remain unqualified until a real permitted run exists.
- Changed release publication to build the multi-architecture image once, promote its OCI archive without digest change, sign the final subject inventory and create a draft only after clean-consumer verification.
- Added strict Helm values schema validation and a non-privileged chart test hook exercised across install, upgrade and rollback.
- Moved required CI lifecycle coverage to the current upstream-supported Kubernetes 1.35, 1.36 and 1.37 minor releases after Kubernetes 1.37 reached GA.
- Aligned local, CI, release and container builds on Go 1.27 and raised the Darwin archive baseline to macOS 13.

### Security

- Updated `golang.org/x/crypto` and its coordinated `golang.org/x/*` module set to releases that address `GO-2026-6303`.
- Expanded the threat model to cover shared-cluster authentication, delegated authorisation, agent token theft, replay, confused-deputy paths, certificate lifecycle and collector compromise.
- Added an opt-in kind feasibility check for aggregation configuration, Pod-bound identity claims and least-privilege namespace, cluster, agent and metrics RBAC.
- Removed the default plaintext ingestion Service port and added projected rotating tokens, exact delegated-auth RBAC and fail-closed ingestion diagnostics.
- Added unbound namespace-viewer, cluster-viewer and metrics-reader roles, server-side scope filtering, direct-identifier denial, scope-bound pagination and multi-user revocation tests.
- Added a disposable-cluster adversarial isolation gate covering direct reachability, NetworkPolicy removal, delegated-authorisation failure, denial timing, bounded read abuse, least privilege and retained-evidence privacy.
- Bound agent operational metrics to loopback and sanitised cgroup scan failures so node-local density and runtime identifiers do not escape through the Pod network or logs.
- Protected `v*` release tags and the reviewed release environment, enabled immutable GitHub releases, and removed persisted write credentials from the publication checkout.

### Upgrade

- Remove retired pre-v1 Helm values before upgrading. Do not use `--reuse-values` across this boundary; render reviewed current values or use Helm's reset-values path.
- Expect one collector restart and loss of its in-memory snapshots, recent history and event-delta baselines. The new generation reports `rebuilding` until agents post fresh evidence.
- Grant the unbound namespace-viewer, cluster-viewer and metrics-reader roles explicitly. The chart does not choose users or service accounts on the operator's behalf.

### Rollback

- In a shared cluster, roll back only to a revision that retains authenticated ingestion and delegated reads. If that boundary cannot be restored, disable KubeMemLens instead of restoring `v0.0.1-alpha.3`.
- The chart has no CRDs, persistent volumes or data migration. Helm rollback restores workloads and configuration but cannot restore discarded in-memory evidence.

### Known limitations

- The collector is one best-effort in-memory replica. High availability and durable history remain deferred.
- Standard AKS, GKE Autopilot, EKS Fargate, AKS virtual nodes, Windows workers and cgroup v1 remain unsupported for deep mode. Provider support never extends beyond the recorded environment versions.
- macOS terminal emulators remain unqualified. The recorded Linux terminal rows and local 5,000-container result do not imply broader terminal or managed-provider scale coverage.

## 0.0.1-alpha.3 - 2026-08-22

### Added

- Prometheus/OpenMetrics collector export with cardinality guardrails and optional ServiceMonitor.
- Versioned snapshot schema, recent `memory.events` deltas, and text/JSON build information.
- Node-filtered informer cache and low-cardinality agent operational metrics.
- Collector ingestion outcome and request-duration metrics.
- CI for formatting, tests, race detection, vet, vulnerability scanning, builds, and Helm validation.
- CI coverage reporting plus digest-pinned configuration, committed-secret, and built-image security scans.
- Bounded Pod history with pressure/event markers and workload roll-ups that retain replica outliers.
- Cached top-level workload owner resolution with explicit doctor and RBAC diagnostics.
- Evidence-based explanation confidence and copyable Pod/workload follow-up commands.
- Independent diagnosis severity, explicit caveats, and exact gauge/counter evidence windows in text, TUI, JSON, YAML, recommendations, capture, and replay paths.
- Redacted, schema-versioned incident capture and bounded offline replay.
- Live Pod comparison and before/after Pod or workload incident comparison with composition deltas and per-signal growth rates.
- Kubectl-style label/field selection, sorting, watch, header suppression, and table/JSON/YAML/CSV top output.
- K9s-style TUI workload drill-down, pause/resume, fast navigation, and on-demand bounded Pod trends.
- TUI 2.0 virtualised tables/details, responsive wide memory dashboard, first-class node navigation, risk filters/sorting, live selected-Pod history and typed read-only incident actions.
- Opt-in three-size PTY qualification with a disposable 20-Pod kind workload and sanitised assertion summary.
- Optional recording/alerting rules, focused runbooks, and a four-panel Grafana dashboard.
- Read-only K9s Pod plugin and versioned JSON/YAML explanation contract without runtime identifiers.
- Composition-aware recommendation export with rationale, guard conditions, and automatic mutation disabled.
- Runtime class, node allocatable memory, and bounded memory-backed `emptyDir` context in Pod explanations.
- `history pod --since` filtering within the bounded in-memory history window.
- Reclaim/refault/major-fault deltas, reclaim efficiency, and per-composition growth rates in history, explanations, and incident comparison.
- Optional eBPF design gate, managed-provider boundary, multi-tenant threat model, benchmark protocol, and ADR-backed implementation deferral.
- An opt-in existing-cluster qualification harness for managed Linux node pools, CRI-O, NetworkPolicy enforcement, immutable image verification, upgrade/rollback, and sanitised evidence.
- A sanitised two-node kind/Calico qualification record proving the harness and default ingestion NetworkPolicy end to end.
- Diagnosis-feedback issue intake for anonymised false/ambiguous findings, plus install-to-first-explanation and per-component density-soak measurements.
- Primary anonymous/file-cache/shmem/residual composition with bounded history, sorting, and machine output, plus kernel, slab, socket, page-table, mapped-file, and THP secondary detail.
- Explicitly pinned Syft release tooling with locally validated non-empty SPDX archive SBOMs.
- Artifact Hub-ready chart README and version-aligned release-image metadata.
- Immutable Dockerfile frontend and Go builder image inputs for reproducible release builds.
- CI compatibility coverage aligned to the then-current upstream-supported Kubernetes 1.34, 1.35 and 1.36 minor releases.
- A digest-pinned Inspektor Gadget prototype evaluation decision that preserves server-side KubeMemLens tenant admission and keeps tracing out of the default chart.

### Changed

- Filesystem-backed cache now excludes shmem/tmpfs; raw cgroup `file` remains observable.
- Compact views now use a non-overlapping residual/other bucket; raw kernel and slab subdivisions remain explicit overlapping drill-down evidence.
- Collector snapshots replace each node atomically, clear missing containers, garbage-collect stale container data, and reject out-of-order updates.
- Collector reads and ingestion use separate ports and a default NetworkPolicy restricts writable ingestion to agent Pods.
- Agent and collector use separate least-privilege ServiceAccounts and hardened security contexts.
- Go, `golang.org/x/text` and the digest-pinned builder were updated to patched releases after reachable-vulnerability scanning.
- TUI snapshot reads now refresh concurrently, and collector storage/API responses have explicit capacity ceilings surfaced by `doctor`.
- TUI defaults to risk-ordered Pods; Tab now changes table/detail focus in wide layouts, while `N/n/w/p/c` select entity views explicitly.
- Helm supports exact image digests and the agent explicitly targets Linux nodes, with operator-scoped tolerations available for reviewed node pools.

### Fixed

- TUI refreshes preserve the terminal title, use synchronized rendering where supported, and report Pod age from Kubernetes creation time rather than snapshot age.

### Security

- Snapshot bodies, entity counts, timestamps, identifiers, JSON shape, headers, and HTTP timeouts are bounded or validated.
- Removed unused host `/proc`, node RBAC, human Pod/log RBAC, collector API token mounting, and ambient Linux capabilities.
