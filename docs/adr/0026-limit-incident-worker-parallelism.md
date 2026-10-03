# ADR 0026: Limit incident worker parallelism

Date: 3 October 2026

Status: implementation candidate; comparative resource qualification pending

## Context

A bounded local diagnostic used the existing candidate image for one confirmed-file
trace. Separate 20-second kernel CPU profiles recorded 199 samples in the Node
service, 692 in its child worker and 609 in the API service. All samples resolved
to symbols, with zero loss and equal enabled/running counters. The worker's futex
syscall appeared in 301 cumulative samples and nanosleep in 133. These counts
overlap; they are not independent shares of CPU time.

The diagnostic used two synthetic fixture Pods without the standard stack. It
identifies scheduler activity as a candidate cost, but cannot establish the cause
of a full resource-budget failure or predict the benefit of changing parallelism.
The worker runs one ring reader and a bounded pipe writer. Its fixed two Go
execution slots, introduced in ADR 0025, warrant a controlled comparison.

## Decision

Set each incident worker's fixed environment to `GOMAXPROCS=1`. Keep the Node
service at two execution slots and retain its shared two-CPU container limit.
Separate workers can still execute concurrently. Keep the fixed heap setting,
credential-free environment, sealed executable, identity validation, deadlines,
output accounting and process containment unchanged.

## Alternatives

Keep two slots if the candidate does not improve measured cost or regresses
delivery, cancellation or supported workloads. Automatic quota discovery remains
unsuitable for the host-mounted cgroup view. Disabling runtime preemption,
reducing target validation frequency or changing acceptance thresholds would
alter unrelated guarantees and is outside this experiment.

## Verification and consequences

One execution slot can reduce simultaneous scheduler handoffs, but may constrain
high-rate throughput or delay other goroutines. The real launcher fixture must
verify both the exact environment and effective parallelism despite conflicting
parent values. Matched artifacts and fresh unchanged idle, active, high-rate,
concurrent, cancellation and provider checks remain required. This decision
grants no performance, supported-profile or release qualification.

## Migration and rollback

The setting applies to newly launched workers after upgrading the matched Node
executable and bundle through the quiescent upgrade procedure. It does not
change persisted data or host policy. Rollback restores the previous matched
bundle, whose launcher fixes two worker execution slots, and verifies cleanup.
