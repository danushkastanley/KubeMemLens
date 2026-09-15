package traceframe

import (
	"bytes"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/trace"
)

func TestControlOOMContextCannotBeSuppliedByNode(t *testing.T) {
	_, first, middle, summary := oomStreamFixture(t)
	last, err := NewSummaryVersion(summary, OOMVersion)
	if err != nil {
		t.Fatal(err)
	}
	zero := uint64(0)
	start, end := *summary.ObservationStartedAt, *summary.ObservationEndedAt
	context := &trace.KubernetesOOMContext{State: "observed", BeforeStart: start.Add(-time.Millisecond), BeforeEnd: start, AfterStart: end, AfterEnd: end.Add(time.Millisecond), Restarts: trace.CounterDelta{State: "reported", Delta: &zero}, PressureBefore: "false", PressureAfter: "true"}
	updated, err := last.WithOOMKubernetesContext(context, end.Add(2*time.Millisecond), 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := updated.WithOOMKubernetesContext(context, end.Add(3*time.Millisecond), 30*time.Second); err == nil {
		t.Fatal("node-supplied Kubernetes context could be overwritten")
	}
	r := NewReader(bytes.NewReader(append(append(encoded(t, first), encoded(t, middle)...), encoded(t, updated)...)))
	for range 3 {
		if _, err := r.Next(); err != nil {
			t.Fatal("control-enriched OOM stream rejected")
		}
	}
	if bytes.Contains(encoded(t, last), []byte("kubernetesContext")) {
		t.Fatal("immutable node frame was changed")
	}
}

func TestOOMAuthorityLossRemainsUnenriched(t *testing.T) {
	frame, err := NewSummaryVersion(Summary{SessionEndedAt: time.Unix(200, 0), Termination: trace.AuthorisationLost, Incomplete: true}, OOMVersion)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := frame.WithOOMKubernetesContext(&trace.KubernetesOOMContext{State: "unavailable"}, time.Unix(201, 0), time.Second)
	if err != nil || !bytes.Equal(encoded(t, frame), encoded(t, updated)) {
		t.Fatal("authority-loss summary acquired context")
	}
}
