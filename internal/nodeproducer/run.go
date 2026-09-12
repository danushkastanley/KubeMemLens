// Package nodeproducer schedules the optional source and authenticated publisher.
package nodeproducer

import (
	"context"
	"errors"
	"math/rand/v2"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/capability"
	"github.com/danushkastanley/kube-memlens/internal/nodecontext"
	"github.com/danushkastanley/kube-memlens/internal/nodestats"
)

type Publisher interface {
	Publish(context.Context, string, api.AgentSnapshot) error
}

type Options struct {
	Now            func() time.Time
	Wait           func(context.Context, time.Duration) error
	Jitter         func() time.Duration
	Report         func(string)
	ObservePublish func(time.Duration, error)
}

// Run requires a successful bounded preflight before recurring collection.
// Only this goroutine owns collection and publication; neither can overlap.
func Run(ctx context.Context, source nodestats.SampleSource, publisher Publisher, opts Options) error {
	if opts.Now == nil {
		opts.Now = func() time.Time { return time.Now().UTC() }
	}
	if opts.Wait == nil {
		opts.Wait = wait
	}
	if opts.Jitter == nil {
		opts.Jitter = func() time.Duration {
			return time.Duration(rand.Int64N(int64(3*time.Second)+1)) - 1500*time.Millisecond
		}
	}
	if opts.Report == nil {
		opts.Report = func(string) {}
	}
	first, err := source.ReadSample(ctx)
	if err != nil {
		return err
	}
	identity := nodecontext.Observation{NodeName: first.Node.NodeName, NodeUID: first.Node.NodeUID}
	delay := nodecontext.CollectionInterval
	report := first
	for {
		snapshot, err := sourceSnapshot(report)
		if err != nil {
			return err
		}
		publishCtx, cancel := context.WithTimeout(ctx, nodecontext.RequestTimeout)
		started := time.Now()
		err = publisher.Publish(publishCtx, report.Node.NodeUID, snapshot)
		if opts.ObservePublish != nil {
			opts.ObservePublish(time.Since(started), err)
		}
		cancel()
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			opts.Report("publish-failed")
			delay = min(delay*2, nodecontext.MaxBackoff)
		} else {
			opts.Report("published")
			if report.Node.Availability == capability.Available {
				delay = nodecontext.CollectionInterval
			}
		}
		jitter := max(-1500*time.Millisecond, min(1500*time.Millisecond, opts.Jitter()))
		if err := opts.Wait(ctx, min(nodecontext.MaxBackoff, delay+jitter)); err != nil {
			return err
		}
		value, readErr := source.ReadSample(ctx)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if readErr != nil {
			report = nodestats.Sample{Node: failure(identity, opts.Now(), readErr)}
			opts.Report(string(report.Node.Reason))
			delay = min(delay*2, nodecontext.MaxBackoff)
			continue
		}
		report = value
		identity = nodecontext.Observation{NodeName: value.Node.NodeName, NodeUID: value.Node.NodeUID}
	}
}

func sourceSnapshot(sample nodestats.Sample) (api.AgentSnapshot, error) {
	result := api.AgentSnapshot{SchemaVersion: api.CurrentSnapshotSchemaVersion, NodeName: sample.Node.NodeName, CapturedAt: sample.Node.ReportedAt, NodeContext: &sample.Node}
	if sample.Volumes != nil {
		body, err := sample.Volumes.EncodePrivate()
		if err != nil {
			return api.AgentSnapshot{}, errors.New("cannot encode bounded volume observation")
		}
		result.VolumeBatch = body
	}
	return result, nil
}

func failure(identity nodecontext.Observation, now time.Time, err error) nodecontext.Observation {
	reason := nodecontext.SourceUnavailable
	var typed *nodestats.Error
	if errors.As(err, &typed) {
		reason = typed.Reason
	}
	availability := capability.Unavailable
	switch reason {
	case nodecontext.Forbidden:
		availability = capability.Forbidden
	case nodecontext.Unsupported:
		availability = capability.Unsupported
	}
	return nodecontext.Observation{NodeName: identity.NodeName, NodeUID: identity.NodeUID, ReportedAt: now, Availability: availability, Reason: reason,
		Evidence: capability.Envelope{Source: nodecontext.Source, APIVersion: "v1alpha1", ReceivedAt: now, Scope: capability.NodeScope,
			Freshness: capability.UnknownFreshness, Completeness: capability.Partial, Stability: capability.ImplementationSpecific}}
}

func wait(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
