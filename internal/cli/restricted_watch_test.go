package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/capability"
	"github.com/danushkastanley/kube-memlens/internal/client"
	"github.com/danushkastanley/kube-memlens/internal/observation"
	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
)

type observationQuery func(context.Context) (observation.Batch, error)

func (q observationQuery) Current(ctx context.Context) (observation.Batch, error) { return q(ctx) }

func watchBatch() observation.Batch {
	value := uint64(32 << 20)
	return observation.Batch{Mode: capability.Restricted, Pods: []observation.Pod{{Namespace: "team-a", Name: "retained", WorkingSet: observation.WorkingSet{Bytes: &value, Availability: capability.Available, Evidence: capability.Envelope{Source: capability.KubernetesMetrics, CapturedAt: time.Now(), Freshness: capability.Fresh, Completeness: capability.Complete}}}}}
}

func watchOptions() topOptions {
	return topOptions{Output: "table", SortBy: "total", Watch: true, WatchInterval: 500 * time.Millisecond}
}

func TestRestrictedWatchKeepsTheLastFrameOnQueryFailure(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	var output bytes.Buffer
	command := &cobra.Command{}
	command.SetContext(ctx)
	command.SetOut(&output)
	calls := 0
	reader := observationQuery(func(context.Context) (observation.Batch, error) {
		calls++
		if calls == 1 {
			return watchBatch(), nil
		}
		if calls == 3 {
			cancel()
		}
		return observation.Batch{}, &capability.SelectionError{Mode: capability.Restricted, Reason: capability.RequestFailed}
	})
	err := runRestrictedTop(command, client.EvidenceSession{Observations: reader}, capability.PodScope, watchOptions(), labels.Everything(), fields.Everything())
	if err != nil || calls != 3 || !strings.Contains(output.String(), "retained") || !strings.Contains(output.String(), "last successful read") || strings.Contains(output.String(), "\x1b[2J") {
		t.Fatalf("watch retention: calls=%d err=%v output=%q", calls, err, output.String())
	}
}

func TestRestrictedWatchClearsDataOnPermissionOrAuthenticationLoss(t *testing.T) {
	for _, reason := range []capability.Reason{capability.AccessDenied, capability.AuthenticationFailed} {
		ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
		var output bytes.Buffer
		command := &cobra.Command{}
		command.SetContext(ctx)
		command.SetOut(&output)
		calls := 0
		reader := observationQuery(func(context.Context) (observation.Batch, error) {
			calls++
			if calls == 1 {
				return watchBatch(), nil
			}
			return observation.Batch{}, &capability.SelectionError{Mode: capability.Restricted, Reason: reason}
		})
		err := runRestrictedTop(command, client.EvidenceSession{Observations: reader}, capability.PodScope, watchOptions(), labels.Everything(), fields.Everything())
		cancel()
		if !client.IsForbidden(err) || calls != 2 || !strings.HasSuffix(output.String(), "\x1b[H\x1b[2J") {
			t.Fatalf("revocation reason=%s calls=%d err=%v", reason, calls, err)
		}
	}
}

type closingOutput struct {
	writes int
	err    error
}

func (w *closingOutput) Write(data []byte) (int, error) {
	w.writes++
	if w.writes > 1 {
		return 0, w.err
	}
	return len(data), nil
}

func TestRestrictedWatchStopsWhenOutputCloses(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	output := &closingOutput{err: errors.New("closed output")}
	command := &cobra.Command{}
	command.SetContext(ctx)
	command.SetOut(output)
	calls := 0
	reader := observationQuery(func(context.Context) (observation.Batch, error) { calls++; return watchBatch(), nil })
	err := runRestrictedTop(command, client.EvidenceSession{Observations: reader}, capability.PodScope, watchOptions(), labels.Everything(), fields.Everything())
	if !errors.Is(err, output.err) || calls != 2 {
		t.Fatalf("output failure retried: calls=%d err=%v", calls, err)
	}
}
