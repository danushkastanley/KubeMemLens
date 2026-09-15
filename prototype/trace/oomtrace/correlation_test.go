package oomtrace

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/trace"
	"github.com/danushkastanley/kube-memlens/prototype/trace/filecache"
)

func fixture(t *testing.T) (trace.Specification, filecache.ObservationWindow, Sample, Sample) {
	t.Helper()
	start := time.Unix(100, 0)
	target := trace.TargetIdentity{Namespace: "tenant", PodName: "private-pod", PodUID: "uid", ContainerName: "worker", ContainerID: strings.Repeat("a", 64), ContainerStartedAt: start.Add(-time.Minute), NodeUID: "node", CgroupID: 123}
	spec, err := trace.NewSpecification(trace.OOM, target, trace.OmitPaths, trace.DefaultBounds())
	if err != nil {
		t.Fatal(err)
	}
	raw := RawSample{Local: []byte("oom 2\noom_kill 1\n"), Hierarchical: []byte("oom 5\noom_kill 3\n"), Current: []byte("0\n"), Limit: []byte("max\n"), Pressure: []byte("some avg10=0.00 avg60=0.00 avg300=0.00 total=5\nfull avg10=0.00 avg60=0.00 avg300=0.00 total=2\n")}
	before, err := NewSample(target, start, start.Add(time.Millisecond), raw)
	if err != nil {
		t.Fatal(err)
	}
	raw.Local = []byte("oom 3\noom_kill 2\n")
	raw.Hierarchical = []byte("oom 8\noom_kill 5\n")
	raw.Limit = []byte("1048576\n")
	after, err := NewSample(target, start.Add(3*time.Second), start.Add(3*time.Second+time.Millisecond), raw)
	if err != nil {
		t.Fatal(err)
	}
	uncertainty := time.Microsecond
	window := filecache.ObservationWindow{Start: start.Add(time.Second), End: start.Add(2 * time.Second), Uncertainty: &uncertainty}
	return spec, window, before, after
}

func TestSeparateLocalHierarchyAndLimits(t *testing.T) {
	spec, window, before, after := fixture(t)
	result := Correlate(spec, window, before, after)
	if result.Window.State != "overlapping" || result.Local.Kill.Delta == nil || *result.Local.Kill.Delta != 1 || result.Hierarchical.Kill.Delta == nil || *result.Hierarchical.Kill.Delta != 2 {
		t.Fatal("local and hierarchical OOM evidence conflated")
	}
	if result.Local.GroupKill.State != "unreported" || result.Local.GroupKill.Delta != nil || result.Current.Before == nil || *result.Current.Before != 0 {
		t.Fatal("missing counter and measured zero conflated")
	}
	if result.LimitBefore.State != "unlimited" || result.LimitBefore.Bytes != nil || result.LimitAfter.State != "finite" || *result.LimitAfter.Bytes != 1048576 {
		t.Fatal("limit states changed")
	}
	if result.PSISome.State != "reported" || *result.PSISome.Delta != 0 {
		t.Fatal("measured zero stall delta lost")
	}
	*result.Current.Before = 99
	if *before.current != 0 {
		t.Fatal("correlation aliases mutable sample")
	}
	if _, err := json.Marshal(before); err == nil || strings.Contains(fmt.Sprint(before), "private-pod") {
		t.Fatal("default formatting retained sample identity")
	}
}

func TestOOMCorrelationUnavailableResetAndWrongLifetime(t *testing.T) {
	spec, window, before, after := fixture(t)
	after.local["oom_kill"] = 0
	after.some = nil
	result := Correlate(spec, window, before, after)
	if result.Local.Kill.State != "reset" || result.Local.Kill.Delta != nil || result.PSISome.State != "unreported" {
		t.Fatal("reset or missing pressure became zero")
	}
	after.target.CgroupID++
	if Correlate(spec, window, before, after).Window.State != "target_changed" {
		t.Fatal("replacement cgroup joined old trace")
	}
	_, _, _, after = fixture(t)
	window.Uncertainty = nil
	if Correlate(spec, window, before, after).Window.State != "clock_uncertain" {
		t.Fatal("unknown clock alignment accepted")
	}
	if Correlate(spec, window, Sample{}, after).Window.State != "unavailable" {
		t.Fatal("missing sample accepted")
	}
}

func TestMalformedAndEmptyOOMSamples(t *testing.T) {
	spec, _, before, _ := fixture(t)
	for _, raw := range []RawSample{{}, {Current: []byte("-1")}, {Limit: []byte("unlimited")}, {Local: []byte("oom bad")}, {Pressure: []byte("some total=1")}, {Current: make([]byte, 16385)}} {
		if _, err := NewSample(spec.Target(), before.start, before.end, raw); err == nil {
			t.Fatal("invalid cgroup sample accepted")
		}
	}
}
