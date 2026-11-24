package cli

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
)

func TestPrintPodHistoryShowsSignalsAndEvents(t *testing.T) {
	output := &bytes.Buffer{}
	printPodHistory(output, []api.PodHistory{{
		Namespace: "default",
		PodName:   "api",
		PodUID:    "1234567890abcdef",
		NodeName:  "node-a",
		Points: []api.MemoryHistoryPoint{{
			CapturedAt:         time.Date(2026, 7, 18, 10, 30, 0, 0, time.Local),
			TotalBytes:         10 << 20,
			ResidualBytes:      2 << 20,
			PSISomeAvg10:       1.25,
			PSIFullAvg10:       0.5,
			OOMKillEventsDelta: 1,
			ReclaimDeltasKnown: true,
			PageScanDelta:      10,
			PageStealDelta:     8,
			RefaultFileDelta:   2,
		}},
	}})
	for _, want := range []string{"Pod: default/api", "Instance: 1234567890ab", "TIME", "OTHER", "10Mi", "2Mi", "1.25/0.50", "kill=1", "eff=80%", "refault=2"} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("history output missing %q:\n%s", want, output.String())
		}
	}
}

func TestFilterHistorySinceKeepsRecentPointsAndInstances(t *testing.T) {
	now := time.Now().UTC()
	series := []api.PodHistory{
		{PodUID: "current", Points: []api.MemoryHistoryPoint{{CapturedAt: now.Add(-10 * time.Minute)}, {CapturedAt: now.Add(-time.Minute)}}},
		{PodUID: "old", Points: []api.MemoryHistoryPoint{{CapturedAt: now.Add(-time.Hour)}}},
	}
	got := filterHistorySince(series, now.Add(-5*time.Minute))
	if len(got) != 1 || got[0].PodUID != "current" || len(got[0].Points) != 1 {
		t.Fatalf("unexpected filtered history: %#v", got)
	}
}

// The TIME column has to line up with the UTC windows printed around it,
// whatever zone a timestamp carries: a capture replayed elsewhere, or days
// later, must read the same.
//
// The points carry a non-UTC zone of their own. The test must not change
// time.Local: other tests in this package read it while running in parallel.
func TestPrintPodHistoryTimesAreDatedUTCWhateverTheirZone(t *testing.T) {
	zone := time.FixedZone("UTC+05:30", 5*3600+30*60)
	output := &bytes.Buffer{}
	printPodHistory(output, []api.PodHistory{{
		Namespace: "default", PodName: "api", NodeName: "node-a",
		Points: []api.MemoryHistoryPoint{
			{CapturedAt: time.Date(2026, 7, 19, 5, 29, 30, 0, zone)},
			{CapturedAt: time.Date(2026, 7, 19, 5, 30, 30, 0, zone)},
		},
	}})

	for _, want := range []string{"TIME (UTC)", "18 Jul 23:59:30", "19 Jul 00:00:30"} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("history output missing %q:\n%s", want, output.String())
		}
	}
}
