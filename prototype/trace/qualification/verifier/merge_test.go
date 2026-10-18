package verifier

import "testing"

func TestMergeOrdersMigratingVerifierAcrossFixedCPUs(t *testing.T) {
	m, _ := NewMerge([]uint32{0, 2})
	for _, frame := range []Frame{
		{Event: Event{Kind: LogFinalized, Time: 20, TID: 42, LogSizeBytes: 10}, CPU: 2, Sequence: 1},
		{Event: Event{Kind: CheckReturn, Time: 30, TID: 42}, CPU: 2, Sequence: 2},
		{Event: Event{Kind: CheckEnter, Time: 10, TID: 42}, CPU: 0, Sequence: 1},
	} {
		if m.Push(frame) != nil {
			t.Fatal("valid frame rejected")
		}
	}
	result, err := m.Finish(40)
	if err != nil || result.CompletedCalls != 1 || result.DurationNanos != 20 || result.FinalizedLogBytes != 10 {
		t.Fatal("migration matching failed")
	}
}

func TestMergeRejectsMissingCPUSequenceAndLateOrAmbiguousRecords(t *testing.T) {
	for _, frame := range []Frame{
		{Event: Event{Kind: CheckEnter, Time: 10, TID: 42}, CPU: 1, Sequence: 1},
		{Event: Event{Kind: CheckEnter, Time: 10, TID: 42}, CPU: 0, Sequence: 2},
		{Event: Event{Kind: CheckEnter, Time: 0, TID: 42}, CPU: 0, Sequence: 1},
	} {
		m, _ := NewMerge([]uint32{0})
		if m.Push(frame) == nil {
			t.Fatal("invalid frame accepted")
		}
		if _, err := m.Snapshot(20); err == nil {
			t.Fatal("failure disappeared")
		}
	}
	m, _ := NewMerge([]uint32{0})
	_, _ = m.Snapshot(20)
	if m.Push(Frame{Event: Event{Kind: CheckEnter, Time: 20, TID: 42}, CPU: 0, Sequence: 1}) == nil {
		t.Fatal("late frame accepted")
	}
	m, _ = NewMerge([]uint32{0, 1})
	_ = m.Push(Frame{Event: Event{Kind: CheckEnter, Time: 20, TID: 42}, CPU: 0, Sequence: 1})
	_ = m.Push(Frame{Event: Event{Kind: LogFinalized, Time: 20, TID: 42}, CPU: 1, Sequence: 1})
	if _, err := m.Snapshot(30); err == nil {
		t.Fatal("ambiguous same-thread timestamps accepted")
	}
}

func TestMergeHasFixedTopologyQueueAndFinalPendingBounds(t *testing.T) {
	for _, cpus := range [][]uint32{nil, {0, 0}, {2, 1}, {4096}} {
		if _, err := NewMerge(cpus); err == nil {
			t.Fatal("invalid CPU roster accepted")
		}
	}
	m, _ := NewMerge([]uint32{0})
	for n := uint64(1); n <= 4096; n++ {
		if m.Push(Frame{Event: Event{Kind: LogFinalized, Time: n, TID: 1}, CPU: 0, Sequence: n}) != nil {
			t.Fatal("queue bound changed")
		}
	}
	if m.Push(Frame{Event: Event{Kind: LogFinalized, Time: 4097, TID: 1}, CPU: 0, Sequence: 4097}) == nil {
		t.Fatal("queue grew unbounded")
	}
	m, _ = NewMerge([]uint32{0})
	_ = m.Push(Frame{Event: Event{Kind: CheckEnter, Time: 1, TID: 1}, CPU: 0, Sequence: 1})
	if _, err := m.Finish(2); err == nil {
		t.Fatal("pending verifier qualified")
	}
}
