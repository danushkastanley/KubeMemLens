# Reliability and availability contract

KubeMemLens uses one in-memory collector. It gives operators explicit evidence state, bounded recovery behaviour and honest failure signals. It does not provide high availability or durable history.

This document defines the component and data contract. Use the [reliability runbook](runbooks/reliability.md) when a state changes or a rollout fails.

## Component health

The collector has separate probes:

| Probe | Port | Contract |
| --- | --- | --- |
| `GET /livez` | Pod-local HTTP `8080` | The collector process can answer HTTP. It does not read snapshots or call Kubernetes. `/healthz` is a pre-v1 compatibility alias. |
| `GET /readyz` | Extension HTTPS `8443` | The extension has usable request-header authentication configuration and can refresh the selected Node inventory and reach the Kubernetes `SubjectAccessReview` API. The probe reads cached check results and does not issue Kubernetes requests per kubelet probe. |

The production Service exposes neither probe. Kubelet calls them inside the Pod network. Liveness must not restart a process because snapshots are missing or the Kubernetes API is temporarily unavailable. Readiness removes the Pod from the `APIService` path when its authentication or delegated-authorisation contract is unavailable.

Readiness does not mean that current memory evidence exists. A ready extension may report `rebuilding`, `degraded` or `stale` data. Clients must inspect the evidence state as well as transport health.

## Lifecycle and evidence states

Startup is a component lifecycle phase. Before the servers start, Kubernetes reports the container as not ready. Once the extension contract is ready, the collector reports one of these evidence states:

| State | Meaning | Operator use |
| --- | --- | --- |
| `rebuilding` | This collector generation has accepted no node snapshots. This is expected after install, restart or upgrade. | Wait for agents. Do not interpret an empty view as zero memory use. |
| `ready` | Every retained node record is within the snapshot TTL and its latest snapshot has complete node, workload and container identity context. | Use the evidence subject to its collection timestamp and normal model caveats. |
| `degraded` | At least one node is fresh, but another node is stale or current evidence is incomplete. Partial DaemonSet rollouts and metadata failures enter this state. | Restrict conclusions to rows marked fresh and complete. Restore the missing node or metadata path. |
| `stale` | The collector retains node evidence, but every retained node record is older than the snapshot TTL. | Treat values as last known. Restore ingestion before making a current-state decision. |
| `unavailable` | The client cannot reach or use the authorised API. This includes transport failure, unavailable `APIService` and revoked access. | The TUI may keep the last rendered rows for context, but it labels them unavailable and disables evidence actions. |

The cluster status includes `generation`, `startedAt`, `transitionedAt`, first and last snapshot times, last receive time, expected, fresh, stale and missing node counts, the last inventory refresh, completeness, `snapshotTTLSeconds` and history reliability. Current container, Pod, namespace and workload records carry freshness and completeness.

Namespace-only callers do not gain access to cluster status. Their client derives the visible state from authorised rows. Server-side namespace filtering remains the security boundary.

## Timestamps and last-known evidence

`capturedAt` is when the agent collected a memory sample. It is not a Pod creation time. Pod age comes from `context.createdAt`; CLI tables label sample age separately.

When a node exceeds the snapshot TTL, the collector keeps its last accepted record and marks it and its aggregates stale. It does not forget an existing Kubernetes Node merely because its agent remains offline. A later accepted snapshot replaces the stale evidence. A successful Node inventory refresh uses the chart's agent node selector and tolerations, removes records for Nodes that Kubernetes has removed, and adds explicit missing coverage for selected Nodes that have not posted yet. The inventory is paged and cannot exceed the configured node ceiling. If no evidence remains, the collector reports `rebuilding` instead of an empty ready view.

Keeping last-known evidence avoids a false empty cluster during an outage. It also means stale values must never be presented as current. Incident captures include reliability state and caveats. A capture is partial when the view is scoped or the collector is not ready.

## History and restart semantics

Current snapshots, event-delta baselines and Pod history live only in the collector process. A collector restart, upgrade or Pod eviction starts a new `generation` and loses all three. An opaque page token from an old generation is invalid.

Collector rollouts use the Deployment `Recreate` strategy. The old generation drains and exits before the new one starts, so the Service never routes across two independent stores or ingestion epochs. This creates a deliberate availability gap during upgrades; it is not zero-downtime or highly available.

History reports:

- `resetAt`, when this process created its history store;
- `availableFrom`, the earliest retained point;
- `completeness`, which remains partial until each returned series has a continuous configured history window and any capacity loss has aged out of that window;
- `droppedSeries`, the number of new Pod series rejected at the series ceiling;
- `evictedPoints`, the number of points removed at the per-series ceiling; and
- `lastLossAt`, the latest series rejection or point eviction in this generation.

The counters remain cumulative for the collector generation, while completeness can recover after `lastLossAt` is outside the requested window. A complete series also needs a current tail. A collection gap longer than the snapshot TTL starts a new continuity window; stopped collection or one point after an outage cannot present old history as complete.

Namespaced history returns only loss counters for that authorised Pod name. The collector keeps at most one scoped series-rejection marker per configured history-series slot, so this explanation path is bounded and cannot expose another namespace's capacity activity.

The defaults retain at most 15 minutes, 181 points per Pod instance and 1,000 series. The extra endpoint lets a five-second series prove a complete inclusive 15-minute window before normal age pruning. One request returns at most 20 Pod instances. These are memory bounds, not a durability promise.

## Retry and shutdown bounds

Agents retry only authenticated epoch reads and snapshot writes after transport errors, HTTP `429` or HTTP `5xx`. Each operation makes at most four attempts. Retry delays use exponential back-off with random jitter between half and all of the current delay. The default delay ceilings are 100 ms, 200 ms and 400 ms. The whole publish operation has a 10-second timeout. A retried write carries the same collector epoch and sequence, so the server can recognise an identical replay.

Agents do not retry validation, authentication or authorisation failures as transient errors. An epoch mismatch causes one epoch refresh before the agent resubmits the same sequence.

On `SIGTERM`, the collector stops readiness before its three-second extension drain delay, shuts down its servers and waits for them. Its shutdown limit is 25 seconds. The chart gives the Pod 30 seconds. The agent cancels scans and publishes through context, then gives its loopback metrics server up to five seconds to close. Kubernetes may still send `SIGKILL` after the configured grace period, so operators must test the exact runtime and admission profile they deploy.

## Default operating and qualification budgets

These values make failure tests measurable. They are defaults, not an availability SLO.

| Behaviour | Default bound or observation point |
| --- | --- |
| Agent scan interval | 5 seconds |
| One scan | 10-second timeout |
| Initial Pod metadata cache sync | 15-second timeout; no snapshot publishes until the cache has synced |
| One authenticated publish, including retries | 10-second timeout |
| Fresh evidence window | 30-second snapshot TTL |
| Readiness authorisation probe | Every 10 seconds, with a 2-second request timeout |
| Selected Node inventory refresh | Every 15 seconds, with a 15-second whole-pagination timeout |
| Kubelet readiness probe | Every 5 seconds; one failed cached check marks the Pod not ready |
| Collector extension request | 10-second timeout |
| Collector shutdown | 25-second internal limit inside a 30-second Pod grace period |
| Extension drain delay | 3 seconds after readiness starts shutting down |
| Default history | 15 minutes, 181 points per Pod instance, 1,000 series |

A release qualification must record four times from the same clock:

1. fault injection;
2. first non-ready, degraded or stale observation;
3. fault removal; and
4. first ready observation with a newer `capturedAt` value.

Record the collector generation before and after a restart. Record `history.resetAt`, `availableFrom`, `droppedSeries` and `evictedPoints`. Do not turn one local timing result into a provider or high-availability claim.

## Failure contract

| Failure | Expected state and recovery |
| --- | --- |
| One agent stops | Its node becomes stale after the TTL. Other fresh nodes make the collector degraded. The state stays degraded while Kubernetes still reports that Node. A successful replacement post returns the node to fresh. |
| Kubernetes API or delegated authorisation is unavailable | `/readyz` fails after the cached readiness probe observes the fault. `/livez` remains successful while the process can serve HTTP. Agents retry only bounded transient operations. |
| Collector restarts | The old Pod becomes unready before drain. The new generation reports rebuilding, with partial history, until agents publish again. |
| Node is replaced | The Node inventory removes the deleted Node record and marks the replacement missing until its DaemonSet agent posts. The collector returns to ready when every selected Node has fresh, complete evidence. |
| Agent rollout is partial | Fresh and stale node records produce degraded, partial coverage. The state returns to ready only when all retained nodes are fresh and complete. |
| Metadata or cgroup walk fails | The agent does not replace a last good snapshot with an unsynchronised or partial walk. Repeated loss eventually makes the retained record stale. Current incomplete node context is marked degraded. |
| History reaches a bound | The collector exposes dropped-series or evicted-point counts and reports history as partial. It does not claim a complete window. |

## Alerts and operator response

Alerts should cover user-visible API availability, collector evidence state, node freshness, ingestion rejection, mapping coverage and return to service. Workload memory alerts remain separate from reliability alerts.

Use the authenticated metrics resource. The production chart does not expose a direct collector metrics route, and loopback agent metrics are not a cluster scrape contract. Keep alert labels bounded. Do not add Pod UID, container ID, cgroup path or credentials.

An alert should link to the [reliability runbook](runbooks/reliability.md) or the narrower [stale-agent runbook](runbooks/agent-snapshot-stale.md). Close an incident only after the API and TUI show the expected state, every intended node has a newer collection timestamp, and history loss is recorded.

## Availability boundary

KubeMemLens does not provide collector failover, replica consistency, durable history, disaster recovery or cross-cluster federation. `collector.replicas` must remain `1`; the chart rejects any other value. Prometheus retention and local incident files belong to the operator and do not make collector state durable.
