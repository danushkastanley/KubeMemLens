package client

import (
	"context"
	"errors"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/capability"
)

// EvidenceSession binds one source selection to one caller and scope. It holds
// no credentials in its public plan, and does not rediscover on refresh failure.
type EvidenceSession struct {
	Plan        capability.Selection
	Reader      SnapshotReader
	Description string
}

func NewEvidenceSession(ctx context.Context, opts Options) (EvidenceSession, error) {
	opts, err := opts.WithDefaults()
	if err != nil {
		return EvidenceSession{}, err
	}
	mode, err := ResolveMode(opts)
	if err != nil {
		return EvidenceSession{}, err
	}
	requested := opts.EvidenceMode
	if requested == capability.Auto && mode != ConnectionModeKubernetesAPI {
		requested = capability.Deep
	}
	session := EvidenceSession{Description: Describe(opts)}
	probes := capability.Probes{
		Deep: capability.ProbeFunc(func(ctx context.Context) (capability.SourceState, error) {
			reader, description, err := NewSnapshotReader(ctx, opts)
			session.Reader, session.Description = reader, description
			if err != nil {
				return evidenceFailure(err), err
			}
			return discoverDeep(ctx, reader)
		}),
		Status:  capability.ProbeFunc(func(ctx context.Context) (capability.SourceState, error) { return discoverStatus(ctx, opts) }),
		Metrics: capability.ProbeFunc(func(ctx context.Context) (capability.SourceState, error) { return discoverMetrics(ctx, opts) }),
	}
	session.Plan, err = capability.Discover(ctx, requested, opts.Timeout, probes)
	if session.Plan.Mode == capability.Restricted {
		session.Reader, session.Description = nil, "Kubernetes APIs"
	}
	return session, err
}

// discoverDeep uses the same scoped read as the query path. API discovery or
// collector health alone cannot prove that the caller can read the namespace.
func discoverDeep(ctx context.Context, reader SnapshotReader) (capability.SourceState, error) {
	if err := reader.Health(ctx); err != nil {
		return evidenceFailure(err), nil
	}
	var pods []api.PodSnapshot
	var err error
	if summaries, ok := reader.(interface {
		PodSummaries(context.Context) ([]api.PodSnapshot, error)
	}); ok {
		pods, err = summaries.PodSummaries(ctx)
	} else {
		pods, err = reader.Pods(ctx)
	}
	if err != nil {
		return evidenceFailure(err), nil
	}
	state := capability.SourceState{Source: capability.Cgroup, Availability: capability.Available,
		APIVersion: api.MemoryAPIGroup + "/" + api.MemoryAPIVersion,
		Freshness:  capability.UnknownFreshness, Completeness: capability.Partial, Stability: capability.Stable}
	if len(pods) == 0 {
		state.Reason = capability.NotObserved
		return state, nil
	}
	state.Freshness, state.Completeness = capability.Fresh, capability.Complete
	now := time.Now()
	for _, pod := range pods {
		switch {
		case pod.Freshness == api.EvidenceFreshnessStale || !pod.CapturedAt.IsZero() && (now.Sub(pod.CapturedAt) > 2*time.Minute || pod.CapturedAt.After(now.Add(5*time.Second))):
			state.Freshness = capability.Stale
		case state.Freshness != capability.Stale && (pod.Freshness != api.EvidenceFreshnessFresh || pod.CapturedAt.IsZero()):
			state.Freshness = capability.UnknownFreshness
		}
		if pod.Completeness != api.EvidenceComplete {
			state.Completeness = capability.Partial
		}
	}
	return state, nil
}

func evidenceFailure(err error) capability.SourceState {
	state := capability.SourceState{Availability: capability.Unavailable, Reason: capability.RequestFailed,
		Freshness: capability.UnknownFreshness, Completeness: capability.Partial}
	var readErr *ReadError
	if !errors.As(err, &readErr) {
		return state
	}
	switch {
	case readErr.StatusCode == 401:
		state.Reason = capability.AuthenticationFailed
	case readErr.Kind == ReadErrorForbidden:
		state.Availability, state.Reason = capability.Forbidden, capability.AccessDenied
	case readErr.Kind == ReadErrorNotFound:
		state.Availability, state.Reason = capability.Absent, capability.SourceAbsent
	case readErr.Kind == ReadErrorUnexpected:
		state.Reason = capability.InvalidResponse
	}
	return state
}
