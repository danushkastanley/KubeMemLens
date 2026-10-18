package kube

import (
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/changemarkers"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestMarkerEventsRetainSeriesTimesWithoutInventingIndividualOccurrences(t *testing.T) {
	f := newMarkerFixture(t)
	first, last := f.now.Add(-2*time.Minute), f.now.Add(-time.Minute)
	f.events[0]["eventTime"] = metav1.NewMicroTime(first)
	f.events[0]["series"] = map[string]any{"count": 3, "lastObservedTime": metav1.NewMicroTime(last)}
	report, err := f.provider.Query(t.Context(), f.selection, f.query)
	if err != nil {
		t.Fatal(err)
	}
	found := 0
	for _, m := range report.Markers {
		if m.Clock == changemarkers.KubernetesEvent {
			found++
			if m.Count != 3 || !m.At.Equal(first) || !m.Until.Equal(last) || !m.Uncertain {
				t.Fatal("event series provenance changed", m)
			}
		}
	}
	if found != 1 {
		t.Fatal("event series expanded into invented occurrences", found)
	}
}

func TestMarkerEventsDoNotTurnMissingOrInvalidEvidenceIntoOneOccurrence(t *testing.T) {
	for _, name := range []string{"missing time", "negative count", "unknown legacy count", "reversed series"} {
		t.Run(name, func(t *testing.T) {
			f := newMarkerFixture(t)
			switch name {
			case "missing time":
				delete(f.events[0], "eventTime")
			case "negative count":
				f.events[0]["count"] = -1
			case "unknown legacy count":
				delete(f.events[0], "eventTime")
				f.events[0]["firstTimestamp"] = metav1.NewTime(f.now.Add(-time.Minute))
				f.events[0]["lastTimestamp"] = metav1.NewTime(f.now)
				f.events[0]["count"] = 0
			case "reversed series":
				f.events[0]["series"] = map[string]any{"count": 2, "lastObservedTime": metav1.NewMicroTime(f.now.Add(-2 * time.Minute))}
			}
			report, err := f.provider.Query(t.Context(), f.selection, f.query)
			if err != nil {
				t.Fatal(err)
			}
			if report.Events != changemarkers.Missing {
				t.Fatal("invalid event evidence invented a count", report.Events)
			}
			for _, m := range report.Markers {
				if m.Clock == changemarkers.KubernetesEvent {
					t.Fatal("invalid count or clock produced a marker", m)
				}
			}
		})
	}
}

func TestMarkerEventsRejectDuplicatesAndFutureClocks(t *testing.T) {
	for _, name := range []string{"duplicate", "future"} {
		t.Run(name, func(t *testing.T) {
			f := newMarkerFixture(t)
			if name == "duplicate" {
				f.events = append(f.events, f.events[0])
			} else {
				f.events[0]["eventTime"] = metav1.NewMicroTime(f.now.Add(time.Minute))
			}
			if _, err := f.provider.Query(t.Context(), f.selection, f.query); err == nil {
				t.Fatal("ambiguous event evidence accepted")
			}
		})
	}
}
