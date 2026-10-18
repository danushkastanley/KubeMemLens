package replicabaseline

import (
	"testing"
	"time"
)

func TestPracticalAndDispersionGates(t *testing.T) {
	for _, test := range []struct {
		name       string
		references []float64
		candidate  float64
		want       string
	}{
		{"exact effect floor", []float64{64, 64, 64, 64}, 80, "none"},
		{"above effect floor", []float64{64, 64, 64, 64}, 81, "higher"},
		{"relative floor", []float64{1024, 1024, 1024, 1024}, 1056, "none"},
		{"wide dispersion", []float64{16, 64, 128, 192}, 256, "none"},
		{"low charge", []float64{64, 64, 64, 64}, 8, "lower"},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := fixture()
			for i, value := range test.references {
				input.Peers[i].Values[Charge] = Value{State: Available, Number: value * (1 << 20)}
			}
			input.Peers[4].Values[Charge] = Value{State: Available, Number: test.candidate * (1 << 20)}
			result, err := Analyse(input)
			if err != nil || metric(t, result.Peers[4], Charge).Outlier != test.want {
				t.Fatalf("comparison=%+v, error=%v", result, err)
			}
		})
	}
}

func history(peer *Peer, points int, value func(int) uint64) {
	peer.HistoryState = Available
	for i := 0; i < points; i++ {
		peer.History = append(peer.History, Point{At: peer.CapturedAt.Add(time.Duration(i-points+1) * 30 * time.Second), Bytes: value(i)})
	}
	peer.Values[Charge] = Value{State: Available, Number: float64(value(points - 1))}
}

func TestSlopeUsesActualSamplesAndRejectsGaps(t *testing.T) {
	input := fixture()
	for i := range input.Peers {
		history(&input.Peers[i], 14, func(int) uint64 { return 64 << 20 })
	}
	input.Peers[4].History = nil
	history(&input.Peers[4], 14, func(i int) uint64 { return (64 + uint64(i)*30) << 20 })
	result, err := Analyse(input)
	if err != nil {
		t.Fatal(err)
	}
	value := metric(t, result.Peers[4], ChargeSlope)
	if value.State != "compared" || value.Candidate == nil || *value.Candidate != 1<<20 || value.Outlier != "higher" {
		t.Fatalf("slope=%+v", value)
	}
	input.Peers[4].History = append(input.Peers[4].History[:6], input.Peers[4].History[7:]...)
	result, err = Analyse(input)
	if err != nil || metric(t, result.Peers[4], ChargeSlope).State != "partial" {
		t.Fatal("history gap became a current slope", err)
	}
}

func TestSlopeDoesNotCompareDifferentRetentionWindows(t *testing.T) {
	input := fixture()
	for i := 0; i < 4; i++ {
		history(&input.Peers[i], 12, func(int) uint64 { return 64 << 20 })
	}
	input.Peers[4].StableSince = input.ObservedAt.Add(-20 * time.Minute)
	history(&input.Peers[4], 21, func(i int) uint64 {
		return (64 + uint64(min(i, 8))*32) << 20
	})
	result, err := Analyse(input)
	if err != nil {
		t.Fatal(err)
	}
	value := metric(t, result.Peers[4], ChargeSlope)
	if value.State != "compared" || value.Candidate == nil || *value.Candidate != 0 || value.Outlier != "none" {
		t.Fatalf("old growth outside the common recent window influenced slope: %+v", value)
	}
}

func TestAdverseRatesRequireComparableIntervals(t *testing.T) {
	input := fixture()
	for i := range input.Peers {
		input.Peers[i].Values[OOMRate] = Value{State: Available}
	}
	input.Peers[4].Values[OOMRate] = Value{State: Available, Number: .2}
	result, err := Analyse(input)
	if err != nil || metric(t, result.Peers[4], OOMRate).Outlier != "higher" {
		t.Fatal("aligned adverse difference missing", err)
	}
	input.Peers[0].DeltaStartedAt = input.Peers[0].CapturedAt.Add(-20 * time.Second)
	result, err = Analyse(input)
	if err != nil {
		t.Fatal(err)
	}
	value := metric(t, result.Peers[4], OOMRate)
	if value.State != "insufficient-peers" || value.Outlier != "none" || len(value.Omitted) != 1 || value.Omitted[0].Reason != "delta-window-mismatch" {
		t.Fatalf("incompatible rate window used: %+v", value)
	}
	if metric(t, result.Peers[4], Charge).Outlier != "higher" {
		t.Fatal("missing rate comparability hid current charge evidence")
	}
}
