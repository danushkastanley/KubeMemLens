package memoryhistory

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestReportWireRejectsNestedExcessAndAmbiguousFields(t *testing.T) {
	for _, data := range []string{
		`{"schemaVersion":1,"SCHEMAVERSION":1}`,
		`{"series":[` + strings.Repeat(`{},`, MaxTargets) + `{}]}`,
		`{"series":[{"points":[` + strings.Repeat(`{},`, MaxPoints) + `{}]}]}`,
		`{"unknown":` + strings.Repeat(`{"nested":`, 10) + `0` + strings.Repeat(`}`, 10) + `}`,
		`{"reason":"` + strings.Repeat("x", 513) + `"}`,
	} {
		var r Report
		if json.Unmarshal([]byte(data), &r) == nil {
			t.Fatal("unbounded or ambiguous history response accepted")
		}
	}
}

func TestReportCannotClaimCompleteCoverageOrAnotherTarget(t *testing.T) {
	for _, mutate := range []func(*Report){
		func(r *Report) { r.Completeness = Complete },
		func(r *Report) { r.Series[0].Target.PodUID = "replacement" },
		func(r *Report) { r.Series[0].Metric = RSS },
		func(r *Report) { r.Series[0].FreshFor = time.Hour },
		func(r *Report) { r.Series[0].SampleClock = "current-time" },
		func(r *Report) { zero := uint64(0); r.Series[0].Points[0].Bytes = &zero },
	} {
		s, q := selected(), query()
		r, err := NewReport(s, q, s.ResolvedAt)
		if err != nil {
			t.Fatal(err)
		}
		r.Series[0].Origin, r.Series[0].SampleClock = "cadvisor", "prometheus-sample"
		if err := r.Validate(); err != nil {
			t.Fatal(err)
		}
		mutate(&r)
		if err := r.Validate(); err == nil {
			t.Fatal("response changed scope or source meaning")
		}
	}
}

func TestQueryDefaultsBoundASevenDayWindow(t *testing.T) {
	now := selected().ResolvedAt
	q, err := ParseQuery(map[string][]string{"source": {"prometheus"}, "start": {now.Add(-MaxRange).Format(time.RFC3339)}}, Pod, now)
	if err != nil || q.Step != 42*time.Minute || q.End.Sub(q.Start)/q.Step+1 > MaxPoints {
		t.Fatalf("seven-day grid: %+v %v", q, err)
	}
}

func FuzzReportDecode(f *testing.F) {
	f.Add([]byte(`{"schemaVersion":1,"series":[]}`))
	f.Add([]byte(`null`))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > MaxResponseBytes+1 {
			return
		}
		var r Report
		if json.Unmarshal(data, &r) == nil {
			if len(r.Series) > MaxTargets || len(r.Selection.Targets) > MaxTargets {
				t.Fatal("target allocation bound lost")
			}
			for _, s := range r.Series {
				if len(s.Points) > MaxPoints {
					t.Fatal("point allocation bound lost")
				}
			}
			_ = r.Validate()
		}
	})
}
