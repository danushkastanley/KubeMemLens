package replicaview

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/changemarkers"
	"github.com/danushkastanley/kube-memlens/internal/replicabaseline"
)

func TestReplicaViewExplainsReferenceSetAndStaleness(t *testing.T) {
	now := time.Now().UTC()
	root := changemarkers.Object{APIVersion: "apps/v1", Kind: "Deployment", Namespace: "team-a", Name: "app", UID: "root"}
	input := replicabaseline.Input{Workload: root, ObservedAt: now}
	for i := 0; i < 5; i++ {
		value := float64(32 << 20)
		if i == 0 {
			value = 96 << 20
		}
		input.Peers = append(input.Peers, replicabaseline.Peer{Object: changemarkers.Object{APIVersion: "v1", Kind: "Pod", Namespace: "team-a", Name: fmt.Sprintf("app-%d", i), UID: fmt.Sprintf("pod-%d", i)}, WorkloadUID: root.UID, Revision: "rs", Shape: strings.Repeat("a", 64), Lifecycle: replicabaseline.Ready, Changes: replicabaseline.ChangeUnknown, SampleState: replicabaseline.Available, HistoryState: replicabaseline.Unreported, CapturedAt: now, StableSince: now.Add(-time.Hour), Values: map[replicabaseline.Metric]replicabaseline.Value{replicabaseline.Charge: {State: replicabaseline.Available, Number: value}}})
	}
	report, err := replicabaseline.Analyse(input)
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Join(Lines(report, now), "\n")
	for _, want := range []string{"higher than peers", "32.00 MiB", "64.00 MiB", "app-1, app-2, app-3, app-4", "limited confidence", "Flat reference values", "unreported", "Effect floors", "Next checks"} {
		if !strings.Contains(text, want) {
			t.Fatal("missing explanation", want)
		}
	}
	stale := strings.Join(Lines(report, now.Add(time.Minute)), "\n")
	if !strings.Contains(stale, "stale") || strings.Contains(stale, "higher than peers") {
		t.Fatal("stale comparison displayed as current")
	}
	report.Peers[0].Comparisons[0].Outlier = "lower"
	if text := strings.Join(Lines(report, now), "\n"); !strings.Contains(text, "invalid") || strings.Contains(text, "lower than peers") {
		t.Fatal("invalid result rendered")
	}
}
