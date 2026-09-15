package filecache

import (
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/trace"
)

func TestCorrelationPreservesWindowsMissingZeroAndReset(t *testing.T) {
	spec := decoder(t, trace.Cache, trace.OmitPaths).spec
	start := time.Unix(20, 0)
	before, err := NewSample(spec.Target(), start, start.Add(time.Millisecond), []byte("file 100\nfile_dirty 0\nworkingset_refault_file 2\npgscan 10\npgsteal 9\n"))
	if err != nil {
		t.Fatal(err)
	}
	after, err := NewSample(spec.Target(), start.Add(3*time.Second), start.Add(3*time.Second+time.Millisecond), []byte("file 90\nfile_dirty 0\nworkingset_refault_file 4\npgscan 1\npgsteal 9\n"))
	if err != nil {
		t.Fatal(err)
	}
	uncertainty := time.Microsecond
	window := ObservationWindow{start.Add(time.Second), start.Add(2 * time.Second), &uncertainty}
	result := Correlate(spec, window, before, after)
	if result.State != "overlapping" || !result.OverlapStart.Equal(window.Start.Add(uncertainty)) || !result.EvidenceStart.Equal(start) {
		t.Fatal("trace overlap and broader sampling interval were conflated")
	}
	if *result.File.Before != 100 || *result.File.After != 90 || *result.Dirty.After != 0 || result.Writeback.After != nil {
		t.Fatal("gauges conflated missing, zero or a falling value")
	}
	if *result.Refault.Delta != 2 || result.Scan.State != "reset" || result.Scan.Delta != nil || *result.Steal.Delta != 0 {
		t.Fatal("counter reset or measured zero was hidden")
	}
	window.Start, window.End = start.Add(4*time.Second), start.Add(5*time.Second)
	if got := Correlate(spec, window, before, after); got.State != "disjoint" || got.File.Before != nil {
		t.Fatal("disjoint evidence was correlated")
	}
	window.Start, window.End, window.Uncertainty = start, start.Add(time.Second), nil
	if got := Correlate(spec, window, before, after); got.State != "clock_uncertain" {
		t.Fatal("unknown alignment treated as exact")
	}
	window.Uncertainty = &uncertainty
	for _, mutate := range []func(*trace.TargetIdentity){
		func(v *trace.TargetIdentity) { v.CgroupID++ },
		func(v *trace.TargetIdentity) { v.PodUID = "replacement" },
		func(v *trace.TargetIdentity) { v.ContainerStartedAt = v.ContainerStartedAt.Add(time.Second) },
		func(v *trace.TargetIdentity) { v.NodeUID = "replacement" },
	} {
		changed := after
		mutate(&changed.target)
		if Correlate(spec, window, before, changed).State != "target_changed" {
			t.Fatal("another lifetime was correlated")
		}
	}
}
