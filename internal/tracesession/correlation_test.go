package tracesession

import (
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/trace"
	"github.com/danushkastanley/kube-memlens/internal/traceaggregate"
)

func TestSummaryCarriesOnlyValidAuthorisedCorrelation(t *testing.T) {
	start, end := time.Unix(200, 0).UTC(), time.Unix(230, 0).UTC()
	uncertainty := time.Microsecond
	acc, err := traceaggregate.New(trace.Files, 10)
	if err != nil {
		t.Fatal(err)
	}
	// Reuse the session fixture's admitted target/bounds; no engine is run.
	fixture := aggregateSession(t, trace.Files, trace.OmitPaths, trace.DefaultBounds(), aggregateObserver(0), &bufferSink{})
	o := output{spec: fixture.metadata.Specification, started: start, deadline: end, aggregates: acc}
	c := &trace.Correlation{State: "overlapping", EvidenceStart: start, BeforeEnd: start, AfterStart: end, EvidenceEnd: end, OverlapStart: start.Add(uncertainty), OverlapEnd: end.Add(-uncertainty), Uncertainty: &uncertainty, Refault: trace.CounterDelta{State: "unreported"}, Scan: trace.CounterDelta{State: "reset"}, Steal: trace.CounterDelta{State: "unreported"}}
	result := trace.Result{Version: trace.ContractVersion, StartedAt: start, EndedAt: end, Termination: trace.Expired, Incomplete: true, Correlation: c}
	summary := o.summary(result, nil, nil, end)
	if summary.Correlation == nil || summary.Correlation.State != "overlapping" {
		t.Fatal("valid correlation lost")
	}
	summary = o.summary(result, nil, Stop(trace.AuthorisationLost), end)
	if summary.Correlation != nil {
		t.Fatal("revocation retained correlation")
	}
	c.EvidenceStart = start.Add(-time.Millisecond)
	summary = o.summary(result, nil, nil, end)
	if summary.Correlation == nil || summary.Correlation.State != "unavailable" || summary.Correlation.File.Before != nil {
		t.Fatal("pre-session evidence retained")
	}
	result.Correlation = &trace.Correlation{State: "clock_uncertain"}
	result.StartedAt, result.EndedAt = time.Time{}, time.Time{}
	summary = o.summary(result, nil, nil, end)
	if summary.Correlation == nil || summary.Correlation.State != "clock_uncertain" {
		t.Fatal("unknown clock reason lost")
	}
}
