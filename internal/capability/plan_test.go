package capability

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

func state(source Source, availability Availability, reason Reason) SourceState {
	return SourceState{Source: source, Availability: availability, Reason: reason,
		Freshness: Fresh, Completeness: Complete, Stability: Stable}
}

func TestPlanDecisionTable(t *testing.T) {
	available := state(Cgroup, Available, "")
	stale := available
	stale.Freshness = Stale
	partial := available
	partial.Completeness = Partial
	status := state(KubernetesStatus, Available, NotObserved)
	metrics := state(KubernetesMetrics, Available, NotObserved)
	for _, test := range []struct {
		name     string
		mode     Mode
		deep     SourceState
		status   SourceState
		wantMode Mode
		wantErr  bool
	}{
		{"healthy deep", Auto, available, status, Deep, false},
		{"stale deep", Auto, stale, status, Deep, false},
		{"partial deep", Auto, partial, status, Deep, false},
		{"absent deep", Auto, state(Cgroup, Absent, SourceAbsent), status, Restricted, false},
		{"forbidden deep", Auto, state(Cgroup, Forbidden, AccessDenied), status, Restricted, false},
		{"explicit restricted", Restricted, available, status, Restricted, false},
		{"explicit deep absent", Deep, state(Cgroup, Absent, SourceAbsent), status, Deep, true},
		{"status forbidden", Restricted, available, state(KubernetesStatus, Forbidden, AccessDenied), Restricted, true},
		{"authentication failure", Auto, state(Cgroup, Unavailable, AuthenticationFailed), status, Deep, true},
		{"transport failure", Auto, state(Cgroup, Unavailable, RequestFailed), status, Deep, true},
		{"malformed deep", Auto, state(Cgroup, Unavailable, InvalidResponse), status, Deep, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			sources := []SourceState{test.deep, test.status, metrics}
			plan, err := Plan(test.mode, sources)
			if (err != nil) != test.wantErr || plan.Mode != test.wantMode {
				t.Fatalf("plan=%+v err=%v", plan, err)
			}
			if !reflect.DeepEqual(plan.Sources, sources) {
				t.Fatal("source reasons or states changed")
			}
			if !test.wantErr && plan.Mode == Deep {
				if plan.Freshness != test.deep.Freshness || plan.Completeness != test.deep.Completeness {
					t.Fatal("lost deep evidence state")
				}
				if err := plan.Require(Current); err != nil {
					t.Fatal(err)
				}
			}
			if plan.Mode == Restricted && !test.wantErr {
				if plan.Completeness != Partial || plan.Require(Composition) == nil || plan.Require(Current) != nil {
					t.Fatal("restricted current query or cgroup exclusion is incorrect")
				}
			}
		})
	}
}

func TestPlanPreservesUnavailableMetricReasons(t *testing.T) {
	for _, availability := range []Availability{Absent, Forbidden, Unsupported, Disabled, Unreported, Unavailable} {
		sources := []SourceState{state(KubernetesStatus, Available, NotObserved), state(KubernetesMetrics, availability, AccessDenied)}
		plan, err := Plan(Restricted, sources)
		if err != nil || plan.Sources[1].Availability != availability || plan.Completeness != Partial {
			t.Fatalf("%+v %v", plan, err)
		}
	}
}

func TestDiscoverProbeOrderAndScope(t *testing.T) {
	for _, test := range []struct {
		mode Mode
		deep Availability
		want []string
	}{
		{Auto, Available, []string{"deep"}},
		{Deep, Absent, []string{"deep"}},
		{Auto, Absent, []string{"deep", "status", "metrics"}},
		{Restricted, Available, []string{"status", "metrics"}},
	} {
		var calls []string
		probe := func(name string, source Source, availability Availability) Probe {
			return ProbeFunc(func(context.Context) (SourceState, error) {
				calls = append(calls, name)
				return state(source, availability, SourceAbsent), nil
			})
		}
		_, _ = Discover(context.Background(), test.mode, time.Second, Probes{
			Deep: probe("deep", Cgroup, test.deep), Status: probe("status", KubernetesStatus, Available), Metrics: probe("metrics", KubernetesMetrics, Available),
		})
		if !reflect.DeepEqual(calls, test.want) {
			t.Fatalf("calls=%v want=%v", calls, test.want)
		}
	}
}

func TestDiscoveryCancellationAndDeadline(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		if cancelled {
			cancel()
		}
		plan, err := Discover(ctx, Restricted, 5*time.Millisecond, Probes{Status: ProbeFunc(func(ctx context.Context) (SourceState, error) {
			<-ctx.Done()
			return SourceState{}, ctx.Err()
		})})
		cancel()
		want := context.DeadlineExceeded
		if cancelled {
			want = context.Canceled
		}
		if !errors.Is(err, want) || plan.State != Unavailable {
			t.Fatalf("plan=%+v err=%v", plan, err)
		}
	}
}

func TestInvalidDiscoveryInputs(t *testing.T) {
	for _, mode := range []Mode{"unexpected", "node-context"} {
		if _, err := Plan(mode, nil); err == nil {
			t.Fatalf("accepted mode %q", mode)
		}
	}
	for _, timeout := range []time.Duration{0, -1, 2 * time.Minute} {
		if _, err := Discover(context.Background(), Auto, timeout, Probes{}); err == nil {
			t.Fatalf("accepted timeout %v", timeout)
		}
	}
	plan, err := Plan(Auto, nil)
	if err == nil || plan.Require(Current) == nil {
		t.Fatal("missing adapters permitted a query")
	}
}

func TestDiscoveryPreservesTypedAdapterFailureWithoutRawErrorText(t *testing.T) {
	cause := errors.New("private provider URL")
	plan, err := Discover(context.Background(), Restricted, time.Second, Probes{Status: ProbeFunc(func(context.Context) (SourceState, error) {
		return state(KubernetesStatus, Unavailable, InvalidResponse), cause
	})})
	var selectionErr *SelectionError
	if !errors.As(err, &selectionErr) || !errors.Is(err, cause) {
		t.Fatalf("error=%v", err)
	}
	if selectionErr.Reason != InvalidResponse || plan.Sources[0].Reason != InvalidResponse {
		t.Fatalf("plan=%+v err=%v", plan, err)
	}
	if err.Error() != "restricted evidence is unavailable: invalid-response" {
		t.Fatalf("error=%v", err)
	}
}
