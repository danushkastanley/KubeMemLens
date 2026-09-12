package nodeproducer

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/capability"
	"github.com/danushkastanley/kube-memlens/internal/nodecontext"
	"github.com/danushkastanley/kube-memlens/internal/nodestats"
)

type sourceFunc func(context.Context) (nodecontext.Observation, error)

func (f sourceFunc) Read(ctx context.Context) (nodecontext.Observation, error) { return f(ctx) }

func (f sourceFunc) ReadSample(ctx context.Context) (nodestats.Sample, error) {
	node, err := f(ctx)
	return nodestats.Sample{Node: node}, err
}

type publisherFunc func(context.Context, string, api.AgentSnapshot) error

func (f publisherFunc) Publish(ctx context.Context, uid string, s api.AgentSnapshot) error {
	return f(ctx, uid, s)
}

func TestPreflightFailureNeverPublishes(t *testing.T) {
	failure := &nodestats.Error{Reason: nodecontext.UntrustedTLS}
	err := Run(context.Background(), sourceFunc(func(context.Context) (nodecontext.Observation, error) { return nodecontext.Observation{}, failure }),
		publisherFunc(func(context.Context, string, api.AgentSnapshot) error {
			t.Fatal("published after failed preflight")
			return nil
		}), Options{})
	if !errors.Is(err, failure) {
		t.Fatal(err)
	}
}

func TestSourceFailurePublishesOnlyKnownIdentityAndRecovers(t *testing.T) {
	now := time.Now().UTC()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reads, posts := 0, 0
	source := sourceFunc(func(context.Context) (nodecontext.Observation, error) {
		reads++
		if reads == 2 {
			return nodecontext.Observation{}, &nodestats.Error{Reason: nodecontext.TimedOut}
		}
		return nodecontext.Observation{NodeName: "node-a", NodeUID: "uid-a", ReportedAt: now, Availability: capability.Available}, nil
	})
	publisher := publisherFunc(func(ctx context.Context, uid string, snapshot api.AgentSnapshot) error {
		posts++
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("publication has no deadline")
		}
		if uid != "uid-a" || len(snapshot.Containers) != 0 || snapshot.NodeContext.NodeName != "node-a" {
			t.Fatal("publisher changed source ownership")
		}
		if posts == 2 {
			value := snapshot.NodeContext
			if value.Reason != nodecontext.TimedOut || value.Stats != nil || !value.Evidence.CapturedAt.IsZero() {
				t.Fatal("failure laundered measurements")
			}
			if err := nodecontext.Validate(*value, now, 2*time.Minute, 30*time.Second); err != nil {
				t.Fatal(err)
			}
		}
		if posts == 3 {
			cancel()
		}
		return nil
	})
	err := Run(ctx, source, publisher, Options{Now: func() time.Time { return now }, Jitter: func() time.Duration { return 0 },
		Wait: func(_ context.Context, d time.Duration) error { now = now.Add(d); return nil }})
	if !errors.Is(err, context.Canceled) || posts != 3 {
		t.Fatalf("posts=%d err=%v", posts, err)
	}
}

func TestPublicationBackoffBoundedAndCancellationStopsCollection(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reads := 0
	source := sourceFunc(func(context.Context) (nodecontext.Observation, error) {
		reads++
		return nodecontext.Observation{NodeName: "node-a", NodeUID: "uid-a", Availability: capability.Available}, nil
	})
	waits := []time.Duration{}
	err := Run(ctx, source, publisherFunc(func(context.Context, string, api.AgentSnapshot) error { return errors.New("unavailable") }), Options{
		Jitter: func() time.Duration { return time.Hour },
		Wait: func(ctx context.Context, d time.Duration) error {
			waits = append(waits, d)
			if len(waits) == 4 {
				cancel()
				return ctx.Err()
			}
			return nil
		},
	})
	if !errors.Is(err, context.Canceled) || reads != 4 {
		t.Fatalf("reads=%d err=%v", reads, err)
	}
	if waits[0] != 31500*time.Millisecond || waits[1] != nodecontext.MaxBackoff || waits[3] != nodecontext.MaxBackoff {
		t.Fatalf("backoff=%v", waits)
	}
}
