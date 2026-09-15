package traceevidence

import (
	"bytes"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/trace"
)

func TestKubernetesOOMContextKeepsReportedFalseUnknownAndReset(t *testing.T) {
	start := time.Unix(100, 0).UTC()
	value := &trace.KubernetesOOMContext{State: "observed", BeforeStart: start, BeforeEnd: start.Add(time.Millisecond), AfterStart: start.Add(time.Second), AfterEnd: start.Add(time.Second + time.Millisecond), Restarts: trace.CounterDelta{State: "reset"}, PressureBefore: "false", PressureAfter: "unknown"}
	data, err := KubernetesOOMEncode(value, 2*time.Second)
	if err != nil || len(data) > MaxKubernetesOOMBytes {
		t.Fatal("Kubernetes OOM context could not encode")
	}
	got, err := KubernetesOOMDecode(data, 2*time.Second)
	if err != nil || got.Restarts.State != "reset" || got.Restarts.Delta != nil || got.PressureBefore != "false" || got.PressureAfter != "unknown" {
		t.Fatal("Kubernetes states changed")
	}
	for _, invalid := range [][]byte{
		bytes.Replace(data, []byte(`"pressureBefore":"false"`), []byte(`"pressureBefore":null`), 1),
		bytes.Replace(data, []byte(`"state":"observed"`), []byte(`"state":"observed","state":"observed"`), 1),
		bytes.Replace(data, []byte(`"pressureAfter":"unknown"`), []byte(`"pressureAfter":"global"`), 1),
		[]byte(`{"state":"unavailable","restarts":{"state":"reported","delta":0}}`),
	} {
		if _, err := KubernetesOOMDecode(invalid, 2*time.Second); err == nil {
			t.Fatal("ambiguous Kubernetes context accepted")
		}
	}
}
