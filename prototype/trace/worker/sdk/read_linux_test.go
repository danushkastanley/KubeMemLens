package sdk

import (
	"math"
	"testing"

	"github.com/danushkastanley/kube-memlens/internal/trace"
)

func counters(produced, sampled, lost, rejected uint64) trace.Counts {
	return trace.Counts{Produced: &produced, Sampled: &sampled, Lost: &lost, Rejected: &rejected}
}

func TestReconcileCountsIncludesUnreadAndRejectedRecords(t *testing.T) {
	// Twenty candidates: two sampled, three lost, one kernel rejection and
	// fourteen ring records. Ten were read, eight passed to the session.
	result, err := reconcileCounts(counters(20, 2, 3, 1), readCounts{seen: 10, forwarded: 8})
	if err != nil || *result.Produced != 20 || *result.Sampled != 2 || *result.Lost != 3 || *result.Rejected != 7 {
		t.Fatal("unread or rejected target observations were hidden")
	}
	// The callback may have rejected its observation; that belongs to the
	// session's rejectedEvents, not another increment in engine rejected.
	result, err = reconcileCounts(counters(1, 0, 0, 0), readCounts{seen: 1, forwarded: 1})
	if err != nil || *result.Rejected != 0 {
		t.Fatal("callback outcome counted twice")
	}
}

func TestInconsistentCountsRemainUnknown(t *testing.T) {
	for _, item := range []struct {
		counts trace.Counts
		reads  readCounts
	}{
		{trace.Counts{}, readCounts{}},
		{counters(1, 0, 0, 0), readCounts{seen: 1, forwarded: 2}},
		{counters(1, 1, 0, 0), readCounts{seen: 1}},
		{counters(1, 2, 0, 0), readCounts{}},
		{counters(math.MaxUint64, math.MaxUint64, 1, 0), readCounts{}},
	} {
		result, err := reconcileCounts(item.counts, item.reads)
		if err == nil || result.Produced != nil || result.Rejected != nil {
			t.Fatal("inconsistent accounting became a known count")
		}
	}
}
