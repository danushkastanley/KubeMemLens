package client

import (
	"context"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/capability"
)

type discoveryReader struct {
	SnapshotReader
	pods []api.PodSnapshot
}

func (r discoveryReader) Health(context.Context) error                    { return nil }
func (r discoveryReader) Pods(context.Context) ([]api.PodSnapshot, error) { return r.pods, nil }

func TestDeepDiscoverySeparatesMissingFreshnessFromStale(t *testing.T) {
	now := time.Now()
	for _, test := range []struct {
		name         string
		pods         []api.PodSnapshot
		freshness    capability.Freshness
		completeness capability.Completeness
	}{
		{"no samples", nil, capability.UnknownFreshness, capability.Partial},
		{"legacy missing freshness", []api.PodSnapshot{{CapturedAt: now}}, capability.UnknownFreshness, capability.Partial},
		{"fresh complete", []api.PodSnapshot{{CapturedAt: now, Freshness: api.EvidenceFreshnessFresh, Completeness: api.EvidenceComplete}}, capability.Fresh, capability.Complete},
		{"expired sample", []api.PodSnapshot{{CapturedAt: now.Add(-3 * time.Minute), Freshness: api.EvidenceFreshnessFresh, Completeness: api.EvidenceComplete}}, capability.Stale, capability.Complete},
		{"mixed unknown stale", []api.PodSnapshot{{}, {CapturedAt: now, Freshness: api.EvidenceFreshnessStale}}, capability.Stale, capability.Partial},
		{"mixed stale unknown", []api.PodSnapshot{{CapturedAt: now, Freshness: api.EvidenceFreshnessStale}, {}}, capability.Stale, capability.Partial},
	} {
		t.Run(test.name, func(t *testing.T) {
			state, err := discoverDeep(context.Background(), discoveryReader{pods: test.pods})
			if err != nil || state.Freshness != test.freshness || state.Completeness != test.completeness {
				t.Fatalf("state=%+v err=%v", state, err)
			}
		})
	}
}
