package workeripc

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/trace"
)

type oomCapture struct {
	capture
	decisions []trace.OOMDecision
}

func (c *oomCapture) OOMDecision(event trace.OOMDecision) error {
	c.decisions = append(c.decisions, event)
	return nil
}

func oomFixture(r Request) trace.OOMDecision {
	command, _ := trace.NewSensitiveText("fixture", 16)
	pid := uint32(1234)
	return trace.OOMDecision{ObservedAt: r.IssuedAt.Add(time.Second), Scope: trace.OOMScopeCgroup, VictimPID: &pid, Command: command}
}

func TestOOMRequestAndEventRoundTrip(t *testing.T) {
	r := requestFixture(t, trace.OOM, trace.OmitPaths, trace.DefaultBounds())
	var request, response bytes.Buffer
	if err := WriteRequest(&request, r); err != nil {
		t.Fatal(err)
	}
	decoded, err := ReadRequest(&request)
	if err != nil || decoded != r {
		t.Fatal("OOM request lost immutable target or bounds")
	}
	w, err := NewWriter(&response, r)
	if err != nil || w.Ready() != nil || w.OOMDecision(oomFixture(r)) != nil || w.Finish(resultFixture(r)) != nil {
		t.Fatal("OOM writer rejected valid bounded sequence")
	}
	output := &oomCapture{}
	ready := false
	result, err := ReadStream(&response, r, output, func() { ready = true })
	if err != nil || !ready || len(output.decisions) != 1 || result.Termination != trace.Expired {
		t.Fatal("OOM response did not round trip")
	}
	event := output.decisions[0]
	if event.Scope != trace.OOMScopeCgroup || event.VictimPID == nil || *event.VictimPID != 1234 || event.Command.Reveal() != "fixture" {
		t.Fatal("OOM event semantics changed")
	}
}

func TestOOMPrivateLimitsAndKindIsolation(t *testing.T) {
	bounds := trace.DefaultBounds()
	bounds.Events = 1
	r := requestFixture(t, trace.OOM, trace.OmitPaths, bounds)
	var response bytes.Buffer
	w, _ := NewWriter(&response, r)
	if w.Ready() != nil || w.OOMDecision(oomFixture(r)) != nil || !errors.Is(w.OOMDecision(oomFixture(r)), ErrLimit) {
		t.Fatal("OOM bypassed private event ceiling")
	}
	for _, mutation := range []string{"wrong-kind", "late", "long-command", "zero-pid", "nul-command"} {
		t.Run(mutation, func(t *testing.T) {
			request := r
			event := oomFixture(r)
			switch mutation {
			case "wrong-kind":
				request = requestFixture(t, trace.Files, trace.OmitPaths, bounds)
			case "late":
				event.ObservedAt = r.Deadline.Add(time.Second)
			case "long-command":
				event.Command, _ = trace.NewSensitiveText(strings.Repeat("x", 17), 512)
			case "zero-pid":
				pid := uint32(0)
				event.VictimPID = &pid
			case "nul-command":
				event.Command, _ = trace.NewSensitiveText("x\x00hidden", 16)
			}
			var buffer bytes.Buffer
			writer, _ := NewWriter(&buffer, request)
			if writer.Ready() != nil {
				t.Fatal("ready failed")
			}
			before := buffer.Len()
			if writer.OOMDecision(event) == nil || buffer.Len() != before {
				t.Fatal("invalid OOM event reached private output")
			}
		})
	}
}
