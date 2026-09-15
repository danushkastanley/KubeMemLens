package traceaggregate

import (
	"errors"
	"testing"

	"github.com/danushkastanley/kube-memlens/internal/trace"
)

func TestOOMCountsPreserveScopeAndMissingContext(t *testing.T) {
	a, err := New(trace.OOM, 3)
	if err != nil {
		t.Fatal(err)
	}
	pid := uint32(1234)
	command, _ := trace.NewSensitiveText("fixture", 16)
	for _, event := range []trace.OOMDecision{
		{Scope: trace.OOMScopeCgroup, VictimPID: &pid, Command: command},
		{Scope: trace.OOMScopeGlobal, VictimPID: &pid},
		{Scope: trace.OOMScopeUnknown, Command: command},
	} {
		if err := a.OOM(event); err != nil {
			t.Fatal(err)
		}
	}
	summary := a.Snapshot()
	if summary.Kind != trace.OOM || summary.Observations != 3 || summary.OOM != (OOMCounts{Cgroup: 1, Global: 1, Unknown: 1, MissingProcessContext: 2}) {
		t.Fatal("OOM scopes or missing context were conflated")
	}
	if !errors.Is(a.OOM(trace.OOMDecision{Scope: trace.OOMScopeUnknown}), ErrLimit) {
		t.Fatal("OOM aggregate exceeded observation bound")
	}
	summary.OOM.Cgroup = 99
	if a.Snapshot().OOM.Cgroup != 1 {
		t.Fatal("snapshot aliases mutable counters")
	}
}

func TestOOMCannotEnterFileAggregateOrHideInvalidContext(t *testing.T) {
	files, _ := New(trace.Files, 1)
	if files.OOM(trace.OOMDecision{Scope: trace.OOMScopeCgroup}) == nil {
		t.Fatal("OOM entered file aggregate")
	}
	a, _ := New(trace.OOM, 1)
	command, _ := trace.NewSensitiveText("x\x00hidden", 16)
	if a.OOM(trace.OOMDecision{Scope: trace.OOMScopeCgroup, Command: command}) == nil || a.Observations() != 0 {
		t.Fatal("invalid context counted as a valid observation")
	}
}
