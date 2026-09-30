package main

import (
	"testing"

	p "github.com/danushkastanley/kube-memlens/internal/tracepreflight"
)

func TestKernelPinRequiresOneMatchingSupportedObservation(t *testing.T) {
	check := p.Check{ID: p.Kernel, State: p.Supported, Reason: p.Available, Value: "6.12.0"}
	valid := p.Report{State: p.Supported, Checks: []p.Check{check}}
	if err := checkRuntimeKernel(valid, "6.12.0"); err != nil {
		t.Fatal("matching kernel rejected", err)
	}
	for _, report := range []p.Report{
		{State: p.Supported},
		{State: p.Degraded, Checks: []p.Check{check}},
		{State: p.Supported, Checks: []p.Check{check, check}},
		{State: p.Supported, Checks: []p.Check{{ID: p.Kernel, State: p.Supported, Reason: p.Available, Value: "6.13.0"}}},
		{State: p.Supported, Checks: []p.Check{{ID: p.Kernel, State: p.Degraded, Reason: p.ProbeFailed, Value: "6.12.0"}}},
	} {
		if err := checkRuntimeKernel(report, "6.12.0"); err == nil {
			t.Fatal("unproven or changed kernel accepted")
		}
	}
	if err := checkRuntimeKernel(valid, "6.12.0\n"); err == nil {
		t.Fatal("invalid kernel pin accepted")
	}
	if err := checkRuntimeKernel(valid, ""); err != nil {
		t.Fatal("historical baseline mode changed", err)
	}
}
