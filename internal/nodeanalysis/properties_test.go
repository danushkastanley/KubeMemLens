package nodeanalysis

import (
	"encoding/json"
	"math"
	"math/rand/v2"
	"reflect"
	"testing"
)

func TestRankingIsDeterministicAcrossInputOrder(t *testing.T) {
	random := rand.New(rand.NewPCG(7, 11))
	for _, metric := range []Metric{Total, Anon, Cache, Shmem, Residual, PSI, OOM} {
		input := testInput()
		input.Rank = metric
		for i := 0; i < 30; i++ {
			c := input.Cgroup.Containers[0]
			c.ID = string(rune('A' + i))
			c.PodUID = c.ID
			c.PodName = "pod-" + c.ID
			c.Charge.Total = uint64(i%3 + 10)
			c.Charge.Anon = uint64(i % 4)
			c.PSIFullAvg10 = fraction(float64(i % 2))
			c.OOMKills = number(uint64(i % 3))
			c.OOMWindowStartedAt = input.Now.Add(-1e9)
			input.Cgroup.Containers = append(input.Cgroup.Containers, c)
		}
		want, err := Analyse(input)
		if err != nil {
			t.Fatal(err)
		}
		for range 50 {
			random.Shuffle(len(input.Cgroup.Containers), func(i, j int) {
				input.Cgroup.Containers[i], input.Cgroup.Containers[j] = input.Cgroup.Containers[j], input.Cgroup.Containers[i]
			})
			got, err := Analyse(input)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(want.Rankings, got.Rankings) {
				t.Fatalf("ranking %s depends on input order", metric)
			}
		}
	}
}

func TestGapArithmeticCannotWrapOrInventNegativePartitions(t *testing.T) {
	random := rand.New(rand.NewPCG(13, 17))
	for range 1000 {
		input := testInput()
		total, pods, system := random.Uint64(), random.Uint64(), random.Uint64()
		input.Current.Stats.Memory.UsageBytes = number(total)
		input.Cgroup.Containers[0].Charge = Charges{Total: pods, Residual: pods}
		input.Current.Stats.SystemContainers[0].Memory.UsageBytes = number(system)
		input.Current.Stats.SystemContainers[1].Memory.UsageBytes = number(0)
		got, err := Analyse(input)
		if err != nil {
			t.Fatal(err)
		}
		outside := uint64(0)
		if total >= pods {
			outside = total - pods
		}
		if got.OutsidePods.Bytes == nil || *got.OutsidePods.Bytes != outside {
			t.Fatal("outside-Pod difference wrapped")
		}
		if system > math.MaxUint64-pods {
			if got.Unaccounted.Bytes != nil {
				t.Fatal("overflowed system sum exposed")
			}
			continue
		}
		expected := uint64(0)
		if total >= pods+system {
			expected = total - pods - system
		}
		if got.Unaccounted.Bytes == nil || *got.Unaccounted.Bytes != expected {
			t.Fatal("unaccounted difference wrapped")
		}
	}
}

func TestInvalidEvidenceReturnsAnErrorInsteadOfUnencodableOutput(t *testing.T) {
	input := testInput()
	input.Current.Stats.Memory.PSI.Full.Avg10 = math.NaN()
	if _, err := Analyse(input); err == nil {
		t.Fatal("NaN PSI accepted")
	}
	input = testInput()
	input.Cgroup.Containers = append(input.Cgroup.Containers, input.Cgroup.Containers[0])
	if _, err := Analyse(input); err == nil {
		t.Fatal("duplicate container admitted")
	}
}

func FuzzNodeAnalysis(f *testing.F) {
	f.Add(uint64(1000), uint64(600), uint64(200), byte(0))
	f.Add(uint64(1), uint64(math.MaxUint64), uint64(1), byte(0))
	f.Fuzz(func(t *testing.T, total, pods, system uint64, flags byte) {
		input := testInput()
		input.Current.Stats.Memory.UsageBytes = number(total)
		input.Cgroup.Containers[0].Charge = Charges{Total: pods, Residual: pods}
		input.Current.Stats.SystemContainers[0].Memory.UsageBytes = number(system)
		input.Current.Stats.SystemContainers[1].Memory.UsageBytes = number(0)
		if flags&1 != 0 {
			input.Access = NodeOnly
		}
		if flags&2 != 0 {
			input.Cgroup.NodeUID = "retired-uid"
		}
		if flags&4 != 0 {
			input.Cgroup.Coverage = Partial
		}
		result, err := Analyse(input)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := json.Marshal(result); err != nil {
			t.Fatal("analysis is not valid machine output", err)
		}
		if input.Access == NodeOnly && (result.Rankings != nil || result.Coverage != nil || result.ObservedPodCharge != nil) {
			t.Fatal("hidden contributor evidence returned")
		}
		for _, estimate := range []Estimate{result.OutsidePods, result.Unaccounted} {
			if estimate.Bytes != nil && *estimate.Bytes > total {
				t.Fatal("estimate exceeded the source usage")
			}
		}
	})
}
