package scheduler

import "testing"

func TestMergeReconstructsMigrationWithoutReadOrderBias(t *testing.T) {
	m, _ := NewMerge([]uint32{0, 1}, 16)
	// CPU1 is drained before CPU0, but the wake timestamp still precedes switch.
	for _, f := range []Frame{{Event{Kind: Switch, Time: 120, NextPID: 10}, 1, 1}, {Event{Kind: Wake, Time: 100, PID: 10}, 0, 1}} {
		if err := m.Push(f); err != nil {
			t.Fatal(err)
		}
	}
	s, err := m.Advance(110)
	if err != nil || s.Pending != 1 || s.Histogram.Count != 0 {
		t.Fatal(s, err)
	}
	s, err = m.Advance(130)
	if err != nil || s.Histogram.Count != 1 || s.Histogram.SumNanos != 20 {
		t.Fatal(s, err)
	}
}

func TestMergeLossLateArrivalSequenceAndRosterCannotRecover(t *testing.T) {
	for _, reason := range []string{"late", "sequence", "cpu", "capacity", "loss", "clock"} {
		t.Run(reason, func(t *testing.T) {
			m, _ := NewMerge([]uint32{0}, 1)
			if err := m.Push(Frame{Event{Kind: Wake, Time: 100, PID: 10}, 0, 1}); err != nil {
				t.Fatal(err)
			}
			var err error
			switch reason {
			case "late":
				_, _ = m.Advance(110)
				err = m.Push(Frame{Event{Kind: Wake, Time: 105, PID: 11}, 0, 2})
			case "sequence":
				err = m.Push(Frame{Event{Kind: Wake, Time: 120, PID: 11}, 0, 3})
			case "cpu":
				err = m.Push(Frame{Event{Kind: Wake, Time: 120, PID: 11}, 1, 1})
			case "capacity":
				err = m.Push(Frame{Event{Kind: Wake, Time: 120, PID: 11}, 0, 2})
			case "loss":
				m.Lost()
			case "clock":
				_, _ = m.Advance(110)
				_, err = m.Advance(109)
			}
			if reason != "loss" && err == nil {
				t.Fatal("invalid stream accepted")
			}
			if _, err = m.Advance(200); err == nil {
				t.Fatal("invalid stream recovered")
			}
		})
	}
}

func TestCrossCPUTiesForSameTaskFailButIndependentTasksDoNot(t *testing.T) {
	for _, same := range []bool{false, true} {
		m, _ := NewMerge([]uint32{0, 1}, 8)
		next := uint32(20)
		if same {
			next = 10
		}
		_ = m.Push(Frame{Event{Kind: Wake, Time: 100, PID: 10}, 0, 1})
		_ = m.Push(Frame{Event{Kind: Switch, Time: 100, NextPID: next}, 1, 1})
		_, err := m.Advance(101)
		if (err != nil) != same {
			t.Fatal("ambiguous timestamp was ordered arbitrarily")
		}
	}
	m, _ := NewMerge([]uint32{0}, 8)
	_ = m.Push(Frame{Event{Kind: Wake, Time: 100, PID: 10}, 0, 1})
	_ = m.Push(Frame{Event{Kind: Switch, Time: 100, NextPID: 10}, 0, 2})
	s, err := m.Advance(101)
	if err != nil || s.Histogram.Count != 1 || s.Histogram.Buckets[0] != 1 {
		t.Fatal(s, err)
	}
}

func TestInvalidMergeBoundsAreRejected(t *testing.T) {
	for _, cpus := range [][]uint32{nil, {0, 0}, {4096}, make([]uint32, 65)} {
		if _, err := NewMerge(cpus, 1); err == nil {
			t.Fatal("invalid CPU roster")
		}
	}
	for _, capacity := range []int{0, -1, 131073} {
		if _, err := NewMerge([]uint32{0}, capacity); err == nil {
			t.Fatal("invalid reorder bound")
		}
	}
}
