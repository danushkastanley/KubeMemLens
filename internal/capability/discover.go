package capability

import (
	"context"
	"errors"
	"fmt"
	"time"
)

type Probes struct{ Deep, Status, Metrics Probe }

// Discover uses a single deadline and a fixed number of probes. Adapters must
// honour cancellation. A usable deep source never probes optional APIs.
func Discover(ctx context.Context, mode Mode, timeout time.Duration, probes Probes) (Selection, error) {
	mode, err := ParseMode(string(mode))
	if err != nil {
		return Selection{}, err
	}
	if timeout <= 0 || timeout > time.Minute {
		return Selection{}, fmt.Errorf("discovery timeout must be between zero and one minute")
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var sources []SourceState
	if mode != Restricted {
		state, err := probe(ctx, Cgroup, probes.Deep)
		sources = append(sources, state)
		if err != nil {
			return failedDiscovery(mode, sources, err)
		}
		if mode == Deep || state.Availability == Available || !fallbackAllowed(state) {
			return Plan(mode, sources)
		}
	}
	status, err := probe(ctx, KubernetesStatus, probes.Status)
	sources = append(sources, status)
	if err != nil {
		return failedDiscovery(mode, sources, err)
	}
	if status.Availability != Available {
		return Plan(mode, sources)
	}
	metrics, err := probe(ctx, KubernetesMetrics, probes.Metrics)
	sources = append(sources, metrics)
	if err != nil {
		return failedDiscovery(mode, sources, err)
	}
	return Plan(mode, sources)
}

func probe(ctx context.Context, source Source, adapter Probe) (SourceState, error) {
	state := SourceState{Source: source, Availability: Unavailable, Reason: NotObserved, Freshness: UnknownFreshness, Completeness: Partial}
	if ctx.Err() != nil {
		state.Reason = discoveryErrorReason(ctx.Err(), state.Reason)
		return state, ctx.Err()
	}
	if adapter == nil {
		return state, nil
	}
	result, err := adapter.Discover(ctx)
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	if err != nil {
		state.Reason = discoveryErrorReason(err, result.Reason)
		return state, err
	}
	result.Source = source
	return result, nil
}

func failedDiscovery(mode Mode, sources []SourceState, err error) (Selection, error) {
	return Selection{Mode: mode, State: Unavailable, Freshness: UnknownFreshness, Completeness: Partial, Sources: sources},
		&SelectionError{Mode: mode, Reason: sources[len(sources)-1].Reason, Cause: err}
}

func discoveryErrorReason(err error, reason Reason) Reason {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return DiscoveryTimedOut
	case errors.Is(err, context.Canceled):
		return DiscoveryCancelled
	case reason != "":
		return reason
	default:
		return RequestFailed
	}
}
