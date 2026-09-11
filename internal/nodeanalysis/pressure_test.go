package nodeanalysis

import (
	"slices"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/nodecontext"
)

func TestNodeSeverityUsesConcreteEvidence(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*Input)
		want   Severity
		signal string
	}{
		{"condition", func(i *Input) { i.Current.Context.MemoryPressure = "True" }, Critical, "kubernetes-memory-pressure"},
		{"some PSI", func(i *Input) { i.Current.Stats.Memory.PSI.Some.Avg10 = 10 }, Warning, "node-some-stall-10s"},
		{"full PSI", func(i *Input) { i.Current.Stats.Memory.PSI.Full.Avg10 = 1 }, Warning, "node-full-stall-10s"},
		{"severe PSI", func(i *Input) { i.Current.Stats.Memory.PSI.Full.Avg10 = 10 }, Critical, "node-full-stall-10s"},
		{"sustained PSI", func(i *Input) { i.Current.Stats.Memory.PSI.Full.Avg60 = 1 }, Critical, "node-sustained-full-stall-60s"},
		{"Pod OOM", func(i *Input) {
			i.Cgroup.Containers[0].OOMKills = number(1)
			i.Cgroup.Containers[0].OOMWindowStartedAt = i.Now.Add(-15 * time.Second)
		}, Warning, "observed-pod-oom-kills"},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := testInput()
			test.change(&input)
			result, err := Analyse(input)
			if err != nil {
				t.Fatal(err)
			}
			if result.Severity != test.want {
				t.Fatalf("severity=%s", result.Severity)
			}
			found := false
			for _, signal := range result.Signals {
				if signal.Code == test.signal && !signal.CapturedAt.IsZero() && signal.Source != "" {
					found = true
				}
			}
			if !found {
				t.Fatal("severity lacks timestamped source evidence")
			}
		})
	}
}

func TestSwapAllocationAloneIsNotPressure(t *testing.T) {
	input := testInput()
	input.Current.Stats.Swap = &nodecontext.Swap{CapturedAt: input.Now, UsageBytes: number(900)}
	result, err := Analyse(input)
	if err != nil {
		t.Fatal(err)
	}
	if result.Severity != Normal || !slices.Contains(result.Caveats, SwapNotIO) {
		t.Fatal("swap allocation became pressure")
	}
}

func TestCounterRatesRequireSameBootAndIncreasingSamples(t *testing.T) {
	input := testInput()
	old := testInput().Current
	old.Stats.Memory.CapturedAt = input.Now.Add(-15 * time.Second)
	old.Stats.Memory.MajorPageFaults = number(10)
	input.Current.Stats.Memory.MajorPageFaults = number(40)
	input.Previous = old
	result, err := Analyse(input)
	if err != nil {
		t.Fatal(err)
	}
	if result.Facts.MajorFaultsPerSecond == nil || *result.Facts.MajorFaultsPerSecond != 2 {
		t.Fatal("wrong rate")
	}
	input.Previous.Stats.StartedAt = input.Previous.Stats.StartedAt.Add(-time.Hour)
	result, err = Analyse(input)
	if err != nil || result.Facts.MajorFaultsPerSecond != nil {
		t.Fatal("counter crossed a boot boundary")
	}
}
