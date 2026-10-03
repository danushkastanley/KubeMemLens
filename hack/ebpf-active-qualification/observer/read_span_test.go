package main

import (
	"errors"
	"fmt"
	"testing"
)

func TestReadSpanUsesTheExistingInclusiveHundredMillisecondBound(t *testing.T) {
	for _, value := range []int64{1, maximumReadNanos - 1, maximumReadNanos} {
		if err := checkReadSpan(2, value, readStages{}); err != nil {
			t.Fatal("valid read rejected", value, err)
		}
	}
	for _, value := range []int64{-1, 0, maximumReadNanos + 1, 206025250} {
		var failure readSpanFailure
		err := checkReadSpan(1215, value, readStages{20, 30, 40, 50})
		if !errors.As(err, &failure) || failure.index != 1215 || failure.nanos != value {
			t.Fatal("invalid read lost its exact numeric evidence", value, err)
		}
		if failureStage(err) != "read-span" || err.Error() != "read-span" {
			t.Fatal("invalid read category changed")
		}
		want := fmt.Sprintf("read-span index=1215 nanos=%d bindings_nanos=20 agent_nanos=30 collector_nanos=40 final_binding_nanos=50", value)
		if failureDetail(err) != want {
			t.Fatal("diagnostic contains unexpected data", failureDetail(err))
		}
	}
}
