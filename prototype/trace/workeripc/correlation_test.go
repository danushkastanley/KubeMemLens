package workeripc

import (
	"bytes"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/trace"
)

func TestPrivateResultCarriesCorrelationAndRejectsAuthorityLoss(t *testing.T) {
	r := requestFixture(t, trace.Cache, trace.OmitPaths, trace.DefaultBounds())
	result := resultFixture(r)
	start, end := r.IssuedAt.Add(time.Millisecond), r.Deadline.Add(-time.Millisecond)
	result.StartedAt, result.EndedAt = start, end
	uncertainty, zero := time.Microsecond, uint64(0)
	result.Correlation = &trace.Correlation{State: "overlapping", EvidenceStart: r.IssuedAt, BeforeEnd: start, AfterStart: end, EvidenceEnd: r.Deadline, OverlapStart: start.Add(uncertainty), OverlapEnd: end.Add(-uncertainty), Uncertainty: &uncertainty, File: trace.GaugePair{Before: &zero, After: &zero}, Refault: trace.CounterDelta{State: "reported", Delta: &zero}, Scan: trace.CounterDelta{State: "reset"}, Steal: trace.CounterDelta{State: "unreported"}}
	var pipe bytes.Buffer
	writer, err := NewWriter(&pipe, r)
	if err != nil || writer.Ready() != nil || writer.Finish(result) != nil {
		t.Fatal("could not write correlation")
	}
	got, err := ReadStream(&pipe, r, &capture{}, func() {})
	if err != nil || got.Correlation == nil || got.Correlation.State != "overlapping" || *got.Correlation.File.After != 0 || got.Correlation.Scan.State != "reset" {
		t.Fatal("private correlation lost")
	}
	for _, mutate := range []func(*trace.Correlation){
		func(c *trace.Correlation) { c.EvidenceStart = r.IssuedAt.Add(-time.Millisecond) },
		func(c *trace.Correlation) { c.EvidenceEnd = r.Deadline.Add(NormalExitGrace + time.Nanosecond) },
	} {
		bad := result
		copy := *result.Correlation
		mutate(&copy)
		bad.Correlation = &copy
		writer, _ = NewWriter(&bytes.Buffer{}, r)
		if writer.Ready() != nil || writer.Finish(bad) == nil {
			t.Fatal("evidence outside private request lifetime accepted")
		}
	}
	// A bounded post-detachment sample remains valid when kernel link closure
	// exceeds the former 500 ms allowance, without extending observation time.
	late := *result.Correlation
	late.AfterStart = r.Deadline.Add(600 * time.Millisecond)
	late.EvidenceEnd = r.Deadline.Add(700 * time.Millisecond)
	result.Correlation = &late
	writer, _ = NewWriter(&bytes.Buffer{}, r)
	if writer.Ready() != nil || writer.Finish(result) != nil {
		t.Fatal("valid sample inside normal exit grace rejected")
	}
	result.Termination = trace.AuthorisationLost
	writer, _ = NewWriter(&bytes.Buffer{}, r)
	if writer.Ready() != nil || writer.Finish(result) == nil {
		t.Fatal("authority loss disclosed correlation")
	}
}
