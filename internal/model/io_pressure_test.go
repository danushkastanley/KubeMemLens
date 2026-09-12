package model

import "testing"

func TestIOPressureDeltasAndInstanceReset(t *testing.T) {
	previous := MemoryBreakdown{IOPressure: IOPressure{State: IOAvailable, Some: PSIWindow{TotalMicros: 100}, Full: PSIWindow{TotalMicros: 20}}}
	current := MemoryBreakdown{IOPressure: IOPressure{State: IOAvailable, Some: PSIWindow{TotalMicros: 150}, Full: PSIWindow{TotalMicros: 25}}}
	p := WithEventDeltas(current, previous, true).IOPressure
	if !p.DeltaKnown || p.SomeDeltaMicros != 50 || p.FullDeltaMicros != 5 || p.CounterReset {
		t.Fatalf("delta = %+v", p)
	}
	if current.IOPressure.DeltaKnown || previous.IOPressure.DeltaKnown {
		t.Fatal("input mutated")
	}
	for _, before := range []IOPressure{{}, {State: IOUnreported}, {State: IOUnavailable}} {
		p := WithEventDeltas(current, MemoryBreakdown{IOPressure: before}, true).IOPressure
		if p.DeltaKnown || p.CounterReset {
			t.Fatalf("missing predecessor = %+v", p)
		}
	}
	reset := WithEventDeltas(previous, current, true).IOPressure
	if !reset.CounterReset || reset.DeltaKnown || reset.SomeDeltaMicros != 0 {
		t.Fatalf("reset = %+v", reset)
	}
	replaced := WithEventDeltas(current, previous, false).IOPressure
	if replaced.DeltaKnown || replaced.CounterReset {
		t.Fatalf("replacement inherited counters: %+v", replaced)
	}
}

func TestIOPressureNeverChangesAggregateMemory(t *testing.T) {
	a := MemoryBreakdown{TotalBytes: 100, AnonBytes: 30, FileBytes: 40, ShmemBytes: 10}
	b := MemoryBreakdown{TotalBytes: 50, AnonBytes: 10, FileBytes: 20, ShmemBytes: 5}
	want := AddMemory(a, b)
	a.IOPressure = IOPressure{State: IOAvailable, Some: PSIWindow{Avg10: 80}}
	b.IOPressure = IOPressure{State: IOAvailable, Some: PSIWindow{Avg10: 60}}
	got := AddMemory(a, b)
	if got != want || got.IOPressure != (IOPressure{}) {
		t.Fatal("I/O entered aggregate memory or summed pressure")
	}
	if a.ResidualBytes() != 30 || a.CacheBytes() != 30 {
		t.Fatal("I/O changed composition")
	}
}
