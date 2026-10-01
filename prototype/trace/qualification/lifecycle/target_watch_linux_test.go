package main

import (
	"bytes"
	"encoding/json"
	"slices"
	"testing"
)

func TestTargetCoverageRetainsDuplicateAndEmptyAssignments(t *testing.T) {
	targets := map[uint64]bool{42: true, 7: true}
	for _, test := range []struct {
		assignments []uint64
		want        []int
	}{{nil, []int{0, 0}}, {[]uint64{42}, []int{0, 1}}, {[]uint64{42, 7}, []int{1, 1}}, {[]uint64{7, 7}, []int{2, 0}}} {
		got, err := coverageCounts(test.assignments, targets)
		if err != nil || !slices.Equal(got, test.want) {
			t.Fatalf("coverage = %v, %v; want %v", got, err, test.want)
		}
	}
	for _, ids := range [][]uint64{{100}, {7, 42, 7}} {
		if _, err := coverageCounts(ids, targets); err == nil {
			t.Fatal("foreign or unbounded assignment accepted")
		}
	}
	for _, selected := range []map[uint64]bool{{7: true}, {7: true, 42: false}, {0: true, 7: true}, {7: true, 42: true, 100: true}} {
		if _, err := coverageCounts(nil, selected); err == nil {
			t.Fatal("invalid target inventory accepted")
		}
	}
}

func TestTargetWatchPreservesUnknownVersusObservedEmpty(t *testing.T) {
	for _, test := range []struct {
		value  *snapshot
		counts []int
	}{{nil, nil}, {&snapshot{Workers: 0}, []int{0, 0}}, {&snapshot{Workers: 2}, []int{1, 1}}} {
		var output bytes.Buffer
		err := watchRecords(t.Context(), &output, 0, 2, func() (*snapshot, []int, error) {
			return test.value, test.counts, nil
		})
		if err != nil {
			t.Fatal(err)
		}
		var row watchRecord
		if json.Unmarshal(output.Bytes(), &row) != nil || row.SchemaVersion != 2 || !slices.Equal(row.TargetWorkers, test.counts) {
			t.Fatal("target observation lost its schema or coverage")
		}
		if test.value == nil && (row.State != "unavailable" || bytes.Contains(output.Bytes(), []byte(`"targetWorkers"`))) {
			t.Fatal("unknown target coverage became an empty observation")
		}
	}
}

func TestInvalidCoverageCannotBeEmitted(t *testing.T) {
	for _, test := range []struct {
		version int
		value   *snapshot
		counts  []int
	}{{1, &snapshot{}, []int{0, 0}}, {2, nil, []int{0, 0}}, {2, &snapshot{}, nil},
		{2, &snapshot{Workers: 2}, []int{1, 0}}, {2, &snapshot{Workers: 2}, []int{-1, 3}}, {3, nil, nil}} {
		var output bytes.Buffer
		err := watchRecords(t.Context(), &output, 0, test.version, func() (*snapshot, []int, error) {
			return test.value, test.counts, nil
		})
		if err == nil || output.Len() != 0 {
			t.Fatal("invalid coverage emitted")
		}
	}
	var output bytes.Buffer
	if watchSamples(t.Context(), &output, 0, func() (*snapshot, error) { return &snapshot{}, nil }) != nil ||
		bytes.Contains(output.Bytes(), []byte(`"targetWorkers"`)) {
		t.Fatal("legacy watch format changed")
	}
}

func TestTargetWatchRequiresTwoTargetsAndBoundedDuration(t *testing.T) {
	for _, seconds := range []int{-1, 0, 1801} {
		if watchTargetsOwned(t.Context(), &bytes.Buffer{}, nil, "", "", "", map[uint64]bool{7: true, 42: true}, seconds) == nil {
			t.Fatal("unbounded target watch")
		}
	}
	if watchTargetsOwned(t.Context(), &bytes.Buffer{}, nil, "", "", "", map[uint64]bool{7: true}, 1) == nil {
		t.Fatal("single target accepted for coverage watch")
	}
}
