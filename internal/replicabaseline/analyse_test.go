package replicabaseline

import (
	"errors"
	"fmt"
	"math"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/changemarkers"
)

func fixture() Input {
	now := time.Date(2026, 9, 29, 0, 0, 0, 0, time.UTC)
	input := Input{ObservedAt: now, Workload: changemarkers.Object{APIVersion: "apps/v1", Kind: "Deployment", Namespace: "team", Name: "app", UID: "workload-uid"}}
	for i := 0; i < 5; i++ {
		input.Peers = append(input.Peers, Peer{Object: changemarkers.Object{APIVersion: "v1", Kind: "Pod", Namespace: "team", Name: fmt.Sprintf("app-%d", i), UID: fmt.Sprintf("pod-%d", i)}, WorkloadUID: "workload-uid", Revision: "revision-1", Shape: strings.Repeat("a", 64), Lifecycle: Ready, SampleState: Available, HistoryState: Unreported, CapturedAt: now.Add(-time.Second), StableSince: now.Add(-10 * time.Minute), Changes: CurrentStable, Values: map[Metric]Value{Charge: {State: Available, Number: 64 << 20}}})
	}
	input.Peers[4].Values[Charge] = Value{State: Available, Number: 256 << 20}
	for i := range input.Peers {
		input.Peers[i].DeltaStartedAt = input.Peers[i].CapturedAt.Add(-5 * time.Second)
	}
	return input
}

func metric(t *testing.T, report PeerReport, name Metric) Comparison {
	t.Helper()
	for _, comparison := range report.Comparisons {
		if comparison.Metric == name {
			return comparison
		}
	}
	t.Fatalf("missing metric %s", name)
	return Comparison{}
}

func TestControlledOutlierUsesOtherPeersWithoutPollutingThem(t *testing.T) {
	input := fixture()
	result, err := Analyse(input)
	if err != nil {
		t.Fatal(err)
	}
	for i, peer := range result.Peers {
		comparison := metric(t, peer, Charge)
		want := "none"
		if i == 4 {
			want = "higher"
		}
		if comparison.Outlier != want || len(comparison.References) != 4 || comparison.ModifiedScore != nil || comparison.Method != "flat-reference" {
			t.Fatalf("peer %d: %+v", i, comparison)
		}
	}
	if input.Peers[4].Values[Charge].Number != 256<<20 {
		t.Fatal("input measurement changed")
	}
	slices.Reverse(input.Peers)
	reordered, err := Analyse(input)
	if err != nil || !reflect.DeepEqual(result, reordered) {
		t.Fatal("input order changed result", err)
	}
}

func TestUnsafeReferenceSetsAbstainWithVisibleReasons(t *testing.T) {
	for _, change := range []string{"small", "revision", "shape", "stale", "partial", "unready", "terminating", "restart", "adverse", "missing-metric", "skew"} {
		t.Run(change, func(t *testing.T) {
			input := fixture()
			peer := &input.Peers[0]
			switch change {
			case "small":
				input.Peers = input.Peers[1:]
			case "revision":
				peer.Revision = "revision-2"
			case "shape":
				peer.Shape = strings.Repeat("b", 64)
			case "stale":
				peer.CapturedAt = input.ObservedAt.Add(-time.Minute)
			case "partial":
				peer.SampleState = Partial
			case "unready":
				peer.Lifecycle = Unready
			case "terminating":
				peer.Lifecycle = Terminating
			case "restart":
				peer.StableSince = input.ObservedAt.Add(-time.Minute)
			case "adverse":
				peer.Values[OOMRate] = Value{State: Available, Number: .2}
			case "missing-metric":
				peer.Values[Charge] = Value{State: Unreported}
			case "skew":
				peer.CapturedAt = peer.CapturedAt.Add(-6 * time.Second)
			}
			result, err := Analyse(input)
			if err != nil {
				t.Fatal(err)
			}
			candidate := result.Peers[len(result.Peers)-1]
			comparison := metric(t, candidate, Charge)
			if comparison.State != "insufficient-peers" || comparison.Outlier != "none" || comparison.Distribution != nil {
				t.Fatalf("unsupported baseline: %+v", comparison)
			}
			if change != "small" && change != "missing-metric" && len(candidate.Excluded) != 1 {
				t.Fatal("reference exclusion lost")
			}
		})
	}
}

func TestScopeAndInvalidNumbersFailClosed(t *testing.T) {
	input := fixture()
	input.Peers[0].Object.Namespace = "other"
	if _, err := Analyse(input); !errors.Is(err, ErrScope) {
		t.Fatal("cross-namespace peer accepted", err)
	}
	for _, value := range []float64{math.NaN(), math.Inf(1), -1, 1.5} {
		input = fixture()
		input.Peers[0].Values[Charge] = Value{State: Available, Number: value}
		if _, err := Analyse(input); !errors.Is(err, ErrInvalid) {
			t.Fatal("invalid measurement accepted", value, err)
		}
	}
}
