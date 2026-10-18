package outputbuffer

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/trace"
	"github.com/danushkastanley/kube-memlens/prototype/trace/workeripc"
)

type fileSink int

func (s *fileSink) FileActivity(trace.FileActivity) error { *s++; return nil }
func (*fileSink) CacheActivity(trace.CacheActivity) error { return workeripc.ErrProtocol }
func (*fileSink) OOMDecision(trace.OOMDecision) error     { return workeripc.ErrProtocol }

func TestProtocolReadinessEventsResultAndEOFThroughPipe(t *testing.T) {
	reader, output, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer output.Close()
	deadline := time.Now().Add(5 * time.Second)
	if reader.SetReadDeadline(deadline) != nil || output.SetWriteDeadline(deadline) != nil {
		t.Fatal("pipe does not support deadlines")
	}
	now := time.Now().UTC()
	bounds := trace.DefaultBounds()
	spec, err := trace.NewSpecification(trace.Files, trace.TargetIdentity{
		Namespace: "tenant", PodName: "pod", PodUID: "pod-uid", ContainerName: "work",
		ContainerID: strings.Repeat("a", 64), ContainerStartedAt: now.Add(-time.Minute),
		NodeUID: "node-uid", CgroupID: 123,
	}, trace.OmitPaths, bounds)
	if err != nil {
		t.Fatal(err)
	}
	request := workeripc.Request{Specification: spec, IssuedAt: now, Deadline: now.Add(bounds.Duration), ManifestSHA256: strings.Repeat("b", 64)}
	ready := make(chan struct{})
	done := make(chan error, 1)
	var received fileSink
	go func() {
		result, err := workeripc.ReadStream(reader, request, &received, func() { close(ready) })
		if err == nil && (result.Counts.Produced == nil || *result.Counts.Produced != 128) {
			err = workeripc.ErrProtocol
		}
		done <- err
	}()
	w := New(output)
	defer w.Abort()
	protocol, err := workeripc.NewWriter(w, request)
	if err != nil || protocol.Ready() != nil || w.Flush() != nil {
		t.Fatal("ready failed")
	}
	select {
	case <-ready:
	case err := <-done:
		t.Fatalf("stream ended before readiness: %v", err)
	case <-time.After(time.Second):
		t.Fatal("readiness depended on later events")
	}
	requested, completed := uint64(4096), uint64(4096)
	for range 128 {
		if err := protocol.FileActivity(trace.FileActivity{ObservedAt: now.Add(time.Second), Operation: trace.FileRead, RequestedBytes: &requested, CompletedBytes: &completed}); err != nil {
			t.Fatal(err)
		}
	}
	produced, zero := uint64(128), uint64(0)
	result := trace.Result{Version: trace.ContractVersion, StartedAt: now, EndedAt: request.Deadline, Termination: trace.Expired, Incomplete: true, Counts: trace.Counts{Produced: &produced, Sampled: &zero, Lost: &zero, Rejected: &zero}}
	if protocol.Finish(result) != nil || w.Flush() != nil {
		t.Fatal("terminal result failed")
	}
	w.Abort()
	if err := output.Close(); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil || received != 128 {
		t.Fatalf("stream result: %v, received %d", err, received)
	}
}
