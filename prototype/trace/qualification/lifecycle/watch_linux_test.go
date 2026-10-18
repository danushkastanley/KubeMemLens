package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"testing"
)

func TestWatchCancellationDoesNotProduceCompletedEvidence(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	var output bytes.Buffer
	called := false
	err := watchSamples(ctx, &output, 1, func() (*snapshot, error) { called = true; return nil, nil })
	if !errors.Is(err, context.Canceled) || called || output.Len() != 0 {
		t.Fatal("cancelled watch continued", err)
	}
}
func TestWatchDistinguishesUncertainAndEmptyOwnedStates(t *testing.T) {
	for _, value := range []*snapshot{nil, {Objects: objectIDs{"map": {}, "prog": {}, "link": {}}}} {
		var output bytes.Buffer
		// Zero seconds is used only at the injected scheduling seam for one record.
		if err := watchSamples(t.Context(), &output, 0, func() (*snapshot, error) { return value, nil }); err != nil {
			t.Fatal(err)
		}
		var row watchRecord
		if err := json.Unmarshal(output.Bytes(), &row); err != nil {
			t.Fatal(err)
		}
		if row.Index != 0 || row.ElapsedNanos != 0 || row.Clock.Monotonic <= 0 || row.ObserverPeakRSSBytes <= 0 {
			t.Fatal(row)
		}
		if value == nil {
			if row.State != "unavailable" || row.Snapshot != nil || bytes.Contains(output.Bytes(), []byte(`"workers"`)) {
				t.Fatal("unknown became zero", row)
			}
		} else if row.State != "observed" || row.Snapshot == nil || row.Snapshot.Workers != 0 {
			t.Fatal("empty observation lost", row)
		}
	}
}
func TestWatchOwnershipFailureAbortsWithoutAnEmptyRecord(t *testing.T) {
	var output bytes.Buffer
	err := watchSamples(t.Context(), &output, 1, func() (*snapshot, error) { return nil, errOwnership })
	if !errors.Is(err, errOwnership) || output.Len() != 0 {
		t.Fatal("ownership failure hidden", err)
	}
	for _, seconds := range []int{0, 1801, -1} {
		if watchOwned(t.Context(), &output, nil, "", "", "", nil, seconds) == nil {
			t.Fatal("invalid duration", seconds)
		}
	}
}

type shortWatchWriter struct{}

func (shortWatchWriter) Write(p []byte) (int, error) { return len(p) - 1, nil }
func TestWatchOutputFailureAndBudgetAbort(t *testing.T) {
	row := watchRecord{State: "unavailable"}
	written := 0
	if !errors.Is(writeWatchRecord(shortWatchWriter{}, &written, row), io.ErrShortWrite) {
		t.Fatal("short write accepted")
	}
	written = maximumWatchBytes
	var output bytes.Buffer
	if writeWatchRecord(&output, &written, row) == nil || output.Len() != 0 {
		t.Fatal("output bound bypassed")
	}
}
