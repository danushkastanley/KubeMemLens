package nodebinding

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/trace"
	"github.com/danushkastanley/kube-memlens/internal/traceframe"
	"github.com/danushkastanley/kube-memlens/prototype/trace/targetfs"
)

type aggregateRuntime struct{}

func (aggregateRuntime) Prepare(context.Context, trace.Specification, targetfs.Handle) (Prepared, error) {
	engine, err := trace.NewEngine(memoryAdapter{})
	return Prepared{engine, tracepreflightDigest(), fixtureProgramme, traceframe.AggregateVersion}, err
}

// This is real mutual TLS and session execution with a test-only observation
// adapter. It does not load a kernel programme or qualify kernel semantics.
func TestAggregateNodeTLSStreamHasNoDefaultEventFrames(t *testing.T) {
	f := setupRuntime(t, aggregateRuntime{})
	id := strings.Repeat("9", 32)
	intent := testIntent()
	binding, err := f.client.Bind(context.Background(), id, workload(), intent, time.Now().Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	defer binding.Close(context.Background())
	deadline := time.Now().Add(300 * time.Millisecond)
	source, err := binding.(StreamBinding).OpenStream(context.Background(), deadline, StreamIdentity{traceframe.AggregateVersion, tracepreflightDigest(), fixtureProgramme})
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	reader := traceframe.NewReader(source)
	first, err := reader.Next()
	if err != nil {
		t.Fatal(err)
	}
	spec, err := trace.NewSpecification(intent.Kind(), binding.Target(), intent.Paths(), intent.Bounds())
	if err != nil {
		t.Fatal(err)
	}
	if first.MatchAdmissionVersion(id, tracepreflightDigest(), fixtureProgramme, spec, deadline, traceframe.AggregateVersion) != nil || first.MatchAdmission(id, tracepreflightDigest(), fixtureProgramme, spec, deadline) == nil {
		t.Fatal("metadata did not bind selected stream version")
	}
	last, err := reader.Next()
	if err != nil || last.Type() != traceframe.SummaryFrame || last.Version() != traceframe.AggregateVersion {
		t.Fatal("missing aggregate terminal")
	}
	data, err := traceframe.Encode(last)
	if err != nil || !strings.Contains(string(data), `"observations":1`) {
		t.Fatal("observed operation lost")
	}
	if _, err := reader.Next(); err != io.EOF {
		t.Fatal("unexpected additional frame")
	}
	if _, events := reader.Counts(); events != 0 {
		t.Fatal("default stream exposed per-operation events")
	}
}

func TestStreamIdentityRequiresExplicitSupportedVersion(t *testing.T) {
	for _, version := range []int{0, 4, -1} {
		if (StreamIdentity{version, tracepreflightDigest(), fixtureProgramme}).valid() {
			t.Fatal("unsupported stream version accepted")
		}
	}
	if !(StreamIdentity{traceframe.OOMVersion, tracepreflightDigest(), fixtureProgramme}).valid() {
		t.Fatal("explicit OOM stream version rejected")
	}
}
