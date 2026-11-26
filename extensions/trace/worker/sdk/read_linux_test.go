package sdk

import (
	"context"
	"encoding/binary"
	"errors"
	"github.com/cilium/ebpf/ringbuf"
	"math"
	"os"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/extensions/trace/filecache"
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

type forwardedFiles struct{ files uint64 }

func (f *forwardedFiles) FileActivity(trace.FileActivity) error {
	f.files++
	return nil
}
func (*forwardedFiles) CacheActivity(trace.CacheActivity) error {
	return errors.New("unexpected cache")
}
func (*forwardedFiles) OOMDecision(trace.OOMDecision) error { return errors.New("unexpected OOM") }

func confirmedFileRecord(path []byte) []byte {
	data := make([]byte, 552)
	binary.LittleEndian.PutUint64(data[:8], 200)
	binary.LittleEndian.PutUint64(data[8:16], 100)
	binary.LittleEndian.PutUint64(data[16:24], 80)
	binary.LittleEndian.PutUint32(data[24:28], 1)
	binary.LittleEndian.PutUint32(data[28:32], uint32(len(path)))
	copy(data[32:], path)
	return data
}

// Linux paths are bytes, not text. One path the stream cannot carry must not
// end the trace; the record is booked as rejected and reading continues.
func TestNonUTF8PathIsRejectedWithoutEndingTrace(t *testing.T) {
	spec, err := trace.NewSpecification(trace.Files, sampleIdentity(), trace.ConfirmedPaths, trace.DefaultBounds())
	if err != nil {
		t.Fatal(err)
	}
	decoder, err := filecache.NewDecoder(spec, filecache.Alignment{MonotonicNS: 100, WallTime: time.Unix(20, 0), Duration: time.Second, Uncertainty: time.Microsecond})
	if err != nil {
		t.Fatal(err)
	}
	output := &forwardedFiles{}
	var reads readCounts
	forwardUntil := time.Now().Add(time.Hour)
	for _, path := range [][]byte{{0xff, 0xfe}, []byte("/fixture")} {
		reads.seen++
		if err := forwardRecord(confirmedFileRecord(path), trace.Files, decoder, output, &reads, forwardUntil); err != nil {
			t.Fatalf("record with path length %d ended the trace: %v", len(path), err)
		}
	}
	counts, err := reconcileCounts(counters(2, 0, 0, 0), reads)
	if err != nil || output.files != 1 || *counts.Rejected != 1 {
		t.Fatalf("unrepresentable path not booked as rejected: err=%v forwarded=%d", err, output.files)
	}
}

func TestCancellationDoesNotHideUnrelatedReaderFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, err := range []error{ringbuf.ErrFlushed, os.ErrDeadlineExceeded} {
		if !interruptedRead(ctx, err) {
			t.Fatal("expected cancellation wake rejected")
		}
		if interruptedRead(context.Background(), err) {
			t.Fatal("unsolicited wake treated as cancellation")
		}
	}
	for _, err := range []error{os.ErrClosed, errors.New("fixture read failure"), nil} {
		if interruptedRead(ctx, err) {
			t.Fatal("cancellation hid unrelated failure")
		}
	}
}
