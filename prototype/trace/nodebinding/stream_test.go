package nodebinding

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/trace"
	admission "github.com/danushkastanley/kube-memlens/internal/traceadmission"
	"github.com/danushkastanley/kube-memlens/internal/traceframe"
)

const fixtureProgramme = "sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"

type memoryRuntime struct{}

func (memoryRuntime) Prepare(context.Context, trace.Specification) (*trace.Engine, string, error) {
	engine, err := trace.NewEngine(memoryAdapter{})
	return engine, fixtureProgramme, err
}

type memoryAdapter struct{}

func (memoryAdapter) Run(ctx context.Context, _ trace.Specification, out trace.Output) (trace.Result, error) {
	start := time.Now().UTC()
	produced, zero := uint64(1), uint64(0)
	err := out.FileActivity(trace.FileActivity{ObservedAt: time.Now().UTC(), Operation: trace.FileRead})
	if err == nil {
		<-ctx.Done()
		err = ctx.Err()
	}
	return trace.Result{Version: 1, StartedAt: start, EndedAt: time.Now().UTC(), Counts: trace.Counts{Produced: &produced, Sampled: &zero, Lost: &zero, Rejected: &zero}}, err
}
func TestRealNodeTLSStreamAndControlConnection(t *testing.T) {
	f := setupRuntime(t, memoryRuntime{})
	w := workload()
	intent := testIntent()
	id := strings.Repeat("a", 32)
	binding, err := f.client.Bind(context.Background(), id, w, intent, time.Now().Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(300 * time.Millisecond)
	source, err := binding.(StreamBinding).OpenStream(context.Background(), deadline)
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
	if err := first.MatchAdmission(id, tracepreflightDigest(), fixtureProgramme, spec, deadline); err != nil {
		t.Fatal(err)
	}
	if err := binding.Revalidate(context.Background()); err != nil {
		t.Fatal("stream starved control request")
	}
	for {
		_, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, events := reader.Counts(); events != 1 {
		t.Fatal("stream lost event")
	}
	if err := binding.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}
func tracepreflightDigest() string {
	return "sha256:bdb8f3ee121d94736570f6554b07d86022d887db9d6e9b9da20f7db31e32a7a1"
}
func TestNodeStreamWithoutApprovedRuntimeIsUnavailable(t *testing.T) {
	f := setup(t)
	binding, err := f.client.Bind(context.Background(), strings.Repeat("b", 32), workload(), testIntent(), time.Now().Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if stream, err := binding.(StreamBinding).OpenStream(context.Background(), time.Now().Add(time.Second)); !errors.Is(err, admission.ErrUnavailable) || stream != nil {
		t.Fatal("unapproved runtime produced a stream")
	}
	if err := binding.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}
func TestNodeStreamDisconnectReleasesHandleAfterEngineExit(t *testing.T) {
	f := setupRuntime(t, memoryRuntime{})
	binding, err := f.client.Bind(context.Background(), strings.Repeat("c", 32), workload(), testIntent(), time.Now().Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	handle := <-f.handles
	source, err := binding.(StreamBinding).OpenStream(context.Background(), time.Now().Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	reader := traceframe.NewReader(source)
	if _, err := reader.Next(); err != nil {
		t.Fatal(err)
	}
	source.Close()
	select {
	case <-handle.closed:
	case <-time.After(2 * time.Second):
		t.Fatal("disconnection retained handle")
	}
}
