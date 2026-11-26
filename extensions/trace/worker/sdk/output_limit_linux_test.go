package sdk

import (
	"bytes"
	"context"
	"encoding/binary"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/extensions/trace/filecache"
	"github.com/danushkastanley/kube-memlens/extensions/trace/workeripc"
	"github.com/danushkastanley/kube-memlens/internal/trace"
)

// The private pipe's byte budget is an admitted bound. A trace that reaches it
// ends as output_limit with its counts, and the supervisor's reader accepts
// that result, rather than reporting an engine failure.
func TestPrivateOutputBudgetEndsAsOutputLimit(t *testing.T) {
	now := time.Now().UTC()
	bounds := trace.DefaultBounds()
	bounds.OutputBytes = 1
	spec, err := trace.NewSpecification(trace.Cache, sampleIdentity(), trace.OmitPaths, bounds)
	if err != nil {
		t.Fatal(err)
	}
	request := workeripc.Request{Specification: spec, IssuedAt: now, Deadline: now.Add(bounds.Duration), ManifestSHA256: strings.Repeat("b", 64)}
	var pipe bytes.Buffer
	writer, err := workeripc.NewWriter(&pipe, request)
	if err != nil || writer.Ready() != nil {
		t.Fatal("protocol writer unavailable")
	}
	clock, err := alignDeadline(request.Deadline)
	if err != nil {
		t.Fatal(err)
	}
	decoder, err := filecache.NewDecoder(spec, clock)
	if err != nil {
		t.Fatal(err)
	}
	var record [24]byte
	binary.LittleEndian.PutUint64(record[:8], clock.MonotonicNS)
	binary.LittleEndian.PutUint64(record[8:16], 1)
	binary.LittleEndian.PutUint32(record[16:20], 1)

	reads := readCounts{seen: 1}
	readErr := forwardRecord(record[:], trace.Cache, decoder, writer, &reads, request.Deadline)
	// readEvents cancels the observation when output refuses a record.
	parent := context.Background()
	ctx, cancel := context.WithDeadline(parent, request.Deadline)
	cancel()
	termination, err := terminalState(ctx, parent, readErr)
	if termination != trace.OutputLimit || err != nil {
		t.Fatalf("exhausted output budget ended as %q with %v (output returned %v)", termination, err, readErr)
	}
	counts, err := reconcileCounts(counters(1, 0, 0, 0), reads)
	if err != nil || *counts.Rejected != 1 {
		t.Fatalf("refused record not booked as rejected: forwarded=%d err=%v", reads.forwarded, err)
	}
	result := trace.Result{Version: trace.ContractVersion, StartedAt: request.IssuedAt, EndedAt: request.Deadline, Termination: termination, Counts: counts, Incomplete: true, Correlation: &trace.Correlation{State: "unavailable"}}
	if err := writer.Finish(result); err != nil {
		t.Fatalf("protocol writer refused the output_limit result: %v", err)
	}
	received, err := workeripc.ReadStream(&pipe, request, &deliveredEvents{}, func() {})
	if err != nil || received.Termination != trace.OutputLimit || received.Counts.Produced == nil || *received.Counts.Produced != 1 {
		t.Fatalf("supervisor reader rejected or changed the output_limit result: %v", err)
	}
}
