package replicabaseline

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"reflect"
	"testing"
	"time"
)

func maximumInput() Input {
	input := fixture()
	prototype := input.Peers[0]
	input.Peers = nil
	for i := 0; i < MaxPeers; i++ {
		peer := prototype
		peer.Object.Name = fmt.Sprintf("replica-application-%02d", i)
		peer.Object.UID = fmt.Sprintf("00000000-0000-4000-8000-%012d", i)
		peer.Values = map[Metric]Value{
			Charge: {Available, 64 << 20}, AnonFraction: {Available, .5},
			FileFraction: {Available, .3}, ShmemFraction: {Available, .1},
			KernelFraction: {Available, .1}, Swap: {Available, 0},
			LimitUsage: {Available, .5}, PSISome: {Available, 0}, PSIFull: {Available, 0},
			OOMRate: {Available, 0}, OOMKillRate: {Available, 0}, HighRate: {Available, 0}, MaxRate: {Available, 0},
		}
		peer.HistoryState = Available
		for n := 0; n < MaxHistoryPoints; n++ {
			peer.History = append(peer.History, Point{At: peer.CapturedAt.Add(time.Duration(n-MaxHistoryPoints+1) * 5 * time.Second), Bytes: 64 << 20})
		}
		input.Peers = append(input.Peers, peer)
	}
	return input
}

func TestMaximumPeerAndHistoryProfileFitsResponseBudget(t *testing.T) {
	input := maximumInput()
	before, _ := json.Marshal(input)
	result, err := Analyse(input)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(result)
	if err != nil || len(encoded) > MaxResponseBytes || len(result.Peers) != 16 {
		t.Fatal("maximum profile did not fit", len(encoded), err)
	}
	for _, peer := range result.Peers {
		if len(peer.Comparisons) != 14 || peer.History == nil || peer.History.Samples != 73 {
			t.Fatal("maximum profile lost metrics or history")
		}
		for _, value := range peer.Comparisons {
			if value.State != "compared" || len(value.References) != 15 || value.Outlier != "none" {
				t.Fatal("complete equal cohort did not compare", value)
			}
		}
	}
	after, _ := json.Marshal(input)
	if string(before) != string(after) {
		t.Fatal("source input mutated")
	}
	t.Logf("maximum encoded response: %d bytes", len(encoded))
}

func TestPermutationAndMissingChangeHistoryPreserveMeaning(t *testing.T) {
	input := fixture()
	input.Peers[0].Changes = ChangeUnknown
	want, err := Analyse(input)
	if err != nil || metric(t, want.Peers[4], Charge).Confidence != "limited-change-history" {
		t.Fatal("missing change history was hidden", err)
	}
	random := rand.New(rand.NewSource(37))
	for i := 0; i < 30; i++ {
		random.Shuffle(len(input.Peers), func(a, b int) { input.Peers[a], input.Peers[b] = input.Peers[b], input.Peers[a] })
		got, err := Analyse(input)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatal("permutation changed evidence", err)
		}
	}
}

func TestOversizedDuplicateAndUnboundEvidenceFailsClosed(t *testing.T) {
	for _, change := range []string{"peers", "history", "name", "uid", "workload", "duplicate-time", "inconsistent-current", "counter-window"} {
		t.Run(change, func(t *testing.T) {
			input := fixture()
			want := ErrInvalid
			switch change {
			case "peers":
				input.Peers = make([]Peer, MaxPeers+1)
				want = ErrBounds
			case "history":
				input.Peers[0].History = make([]Point, MaxHistoryPoints+1)
				want = ErrBounds
			case "name":
				input.Peers[1].Object.Name = input.Peers[0].Object.Name
			case "uid":
				input.Peers[1].Object.UID = input.Peers[0].Object.UID
			case "workload":
				input.Peers[0].WorkloadUID = "other"
				want = ErrScope
			case "duplicate-time":
				point := Point{At: input.Peers[0].CapturedAt, Bytes: 64 << 20}
				input.Peers[0].History = []Point{point, point}
			case "inconsistent-current":
				input.Peers[0].HistoryState = Available
				input.Peers[0].History = []Point{{At: input.Peers[0].CapturedAt, Bytes: 1}}
			case "counter-window":
				input.Peers[0].Values[OOMRate] = Value{Available, .2}
				input.Peers[0].DeltaStartedAt = time.Time{}
			}
			if _, err := Analyse(input); !errors.Is(err, want) {
				t.Fatalf("error=%v, want %v", err, want)
			}
		})
	}
}
