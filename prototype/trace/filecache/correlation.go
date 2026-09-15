package filecache

import (
	"errors"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/cgroup"
	"github.com/danushkastanley/kube-memlens/internal/trace"
)

var ErrSample = errors.New("invalid trace cgroup sample")

// Sample retains only the six requested memory.stat fields and exact lifetime.
// Producers must read through the retained cgroup handle and revalidate it.
type Sample struct {
	target     trace.TargetIdentity
	start, end time.Time
	values     map[string]uint64
}

func NewSample(target trace.TargetIdentity, start, end time.Time, memoryStat []byte) (Sample, error) {
	if target.ValidateLifetime() != nil || target.CgroupID == 0 || start.IsZero() || start.Before(target.ContainerStartedAt) || end.Before(start) || end.Sub(start) > time.Second || len(memoryStat) == 0 || len(memoryStat) > 16384 {
		return Sample{}, ErrSample
	}
	stat, err := cgroup.ParseMemoryStat(memoryStat)
	if err != nil {
		return Sample{}, ErrSample
	}
	values := make(map[string]uint64, 6)
	for _, key := range []string{"file", "file_dirty", "file_writeback", "workingset_refault_file", "pgscan", "pgsteal"} {
		if value, present := stat.Values[key]; present {
			values[key] = value
		}
	}
	target.ContainerStartedAt = target.ContainerStartedAt.UTC()
	return Sample{target, start.UTC(), end.UTC(), values}, nil
}

type ObservationWindow struct {
	Start, End  time.Time
	Uncertainty *time.Duration // nil is unknown, never an exact alignment
}
type GaugePair = trace.GaugePair
type CounterDelta = trace.CounterDelta
type Correlation = trace.Correlation

// Correlate establishes temporal overlap, not causation or page ownership. Deltas
// cover the full cgroup sampling interval; they are never scaled to the overlap
// or added to memory composition. No event/path data is accepted or retained.
func Correlate(spec trace.Specification, window ObservationWindow, before, after Sample) Correlation {
	if spec.Validate() != nil || (spec.Kind() != trace.Files && spec.Kind() != trace.Cache) || before.start.IsZero() || after.start.IsZero() || !after.start.After(before.end) || after.end.Sub(before.start) > spec.Bounds().Duration+2*time.Second {
		return Correlation{State: "unavailable"}
	}
	if spec.Target() != before.target || spec.Target() != after.target {
		return Correlation{State: "target_changed"}
	}
	if window.Start.IsZero() || window.Start.Before(spec.Target().ContainerStartedAt) || !window.End.After(window.Start) || window.End.Sub(window.Start) > spec.Bounds().Duration || window.Uncertainty == nil || *window.Uncertainty < 0 || *window.Uncertainty > 5*time.Millisecond {
		return Correlation{State: "clock_uncertain"}
	}
	start, end := window.Start.Add(*window.Uncertainty), window.End.Add(-*window.Uncertainty)
	if before.end.After(start) {
		start = before.end
	}
	if after.start.Before(end) {
		end = after.start
	}
	if !end.After(start) {
		return Correlation{State: "disjoint"}
	}
	uncertainty := *window.Uncertainty
	result := Correlation{State: "overlapping", EvidenceStart: before.start, BeforeEnd: before.end, AfterStart: after.start, EvidenceEnd: after.end, Uncertainty: &uncertainty,
		OverlapStart: start, OverlapEnd: end,
		File: gauge(before, after, "file"), Dirty: gauge(before, after, "file_dirty"), Writeback: gauge(before, after, "file_writeback"),
		Refault: delta(before, after, "workingset_refault_file"), Scan: delta(before, after, "pgscan"), Steal: delta(before, after, "pgsteal")}
	if result.Validate(window.Start, window.End, spec.Bounds().Duration) != nil {
		return Correlation{State: "unavailable"}
	}
	return result
}
func value(sample Sample, key string) *uint64 {
	v, ok := sample.values[key]
	if !ok {
		return nil
	}
	return &v
}
func gauge(before, after Sample, key string) GaugePair {
	return GaugePair{Before: value(before, key), After: value(after, key)}
}
func delta(before, after Sample, key string) CounterDelta {
	a, b := value(before, key), value(after, key)
	if a == nil || b == nil {
		return CounterDelta{State: "unreported"}
	}
	if *b < *a {
		return CounterDelta{State: "reset"}
	}
	difference := *b - *a
	return CounterDelta{State: "reported", Delta: &difference}
}
