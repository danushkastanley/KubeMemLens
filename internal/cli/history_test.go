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

// The reader may sit in any time zone, or replay a capture days later. The
// TIME column has to line up with the UTC windows printed around it.
func TestPrintPodHistoryTimesAreDatedUTCWhateverTheLocalZone(t *testing.T) {
	local := time.Local
	time.Local = time.FixedZone("UTC+05:30", 5*3600+30*60)
	t.Cleanup(func() { time.Local = local })

	output := &bytes.Buffer{}
	printPodHistory(output, []api.PodHistory{{
		Namespace: "default", PodName: "api", NodeName: "node-a",
		Points: []api.MemoryHistoryPoint{
			{CapturedAt: time.Date(2026, 7, 18, 23, 59, 30, 0, time.UTC)},
			{CapturedAt: time.Date(2026, 7, 19, 0, 0, 30, 0, time.UTC)},
		},
	}})

	for _, want := range []string{"TIME (UTC)", "18 Jul 23:59:30", "19 Jul 00:00:30"} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("history output missing %q:\n%s", want, output.String())
		}
	}
}
