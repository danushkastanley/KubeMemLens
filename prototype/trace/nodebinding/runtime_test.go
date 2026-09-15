package nodebinding

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/trace"
	admission "github.com/danushkastanley/kube-memlens/internal/traceadmission"
	"github.com/danushkastanley/kube-memlens/internal/traceframe"
	"github.com/danushkastanley/kube-memlens/prototype/trace/targetfs"
)

type inspectingRuntime struct {
	handles chan targetfs.Handle
	digest  string
}

func (r inspectingRuntime) Prepare(ctx context.Context, spec trace.Specification, handle targetfs.Handle) (Prepared, error) {
	if handle == nil || handle.Target() != spec.Target() || handle.Check(ctx) != nil {
		return Prepared{}, admission.ErrTargetChanged
	}
	r.handles <- handle
	engine, err := trace.NewEngine(memoryAdapter{})
	return Prepared{engine, r.digest, fixtureProgramme, traceframe.Version}, err
}

func TestRuntimeBorrowsExactHandleAndReportsSelectedEngine(t *testing.T) {
	digest := "sha256:" + strings.Repeat("7", 64)
	seen := make(chan targetfs.Handle, 1)
	f := setupRuntime(t, inspectingRuntime{seen, digest})
	id := strings.Repeat("d", 32)
	intent := testIntent()
	binding, err := f.client.Bind(context.Background(), id, workload(), intent, time.Now().Add(3*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	defer binding.Close(context.Background())
	handle := <-f.handles
	deadline := time.Now().Add(time.Second)
	source, err := binding.(StreamBinding).OpenStream(context.Background(), deadline, StreamIdentity{traceframe.Version, digest, fixtureProgramme})
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	first, err := traceframe.NewReader(source).Next()
	if err != nil {
		t.Fatal(err)
	}
	if <-seen != handle {
		t.Fatal("runtime received another target handle")
	}
	select {
	case <-handle.closed:
		t.Fatal("preparation closed borrowed handle")
	default:
	}
	spec, err := trace.NewSpecification(intent.Kind(), binding.Target(), intent.Paths(), intent.Bounds())
	if err != nil {
		t.Fatal(err)
	}
	if first.MatchAdmission(id, digest, fixtureProgramme, spec, deadline) != nil {
		t.Fatal("selected engine identity lost")
	}
	if first.MatchAdmission(id, tracepreflightDigest(), fixtureProgramme, spec, deadline) == nil {
		t.Fatal("old engine identity accepted for custom runtime")
	}
}

func TestInvalidPreparedIdentityCannotActivateLease(t *testing.T) {
	seen := make(chan targetfs.Handle, 1)
	f := setupRuntime(t, inspectingRuntime{seen, "invalid"})
	binding, err := f.client.Bind(context.Background(), strings.Repeat("e", 32), workload(), testIntent(), time.Now().Add(3*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	handle := <-f.handles
	source, err := binding.(StreamBinding).OpenStream(context.Background(), time.Now().Add(time.Second), StreamIdentity{traceframe.Version, tracepreflightDigest(), fixtureProgramme})
	if !errors.Is(err, admission.ErrUnavailable) || source != nil {
		t.Fatal("invalid runtime metadata started stream")
	}
	if <-seen != handle {
		t.Fatal("runtime did not receive retained handle")
	}
	select {
	case <-handle.closed:
		t.Fatal("failed preparation stole handle ownership")
	default:
	}
	if err := binding.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-handle.closed:
	case <-time.After(time.Second):
		t.Fatal("pending lease did not release handle")
	}
}

func TestControllerIdentityMismatchIsRejectedBeforeActivation(t *testing.T) {
	for _, field := range []string{"engine", "programme", "stream version"} {
		t.Run(field, func(t *testing.T) {
			f := setupRuntime(t, memoryRuntime{})
			id := strings.Repeat("f", 32)
			binding, err := f.client.Bind(context.Background(), id, workload(), testIntent(), time.Now().Add(3*time.Second))
			if err != nil {
				t.Fatal(err)
			}
			defer binding.Close(context.Background())
			handle := <-f.handles
			expected := StreamIdentity{traceframe.Version, tracepreflightDigest(), fixtureProgramme}
			switch field {
			case "engine":
				expected.EngineDigest = "sha256:" + strings.Repeat("7", 64)
			case "programme":
				expected.ProgrammeDigest = "sha256:" + strings.Repeat("8", 64)
			case "stream version":
				expected.StreamVersion = traceframe.AggregateVersion
			}
			source, err := binding.(StreamBinding).OpenStream(context.Background(), time.Now().Add(time.Second), expected)
			if !errors.Is(err, admission.ErrUnavailable) || source != nil {
				t.Fatal("mismatched controller identity received stream")
			}
			f.service.mu.Lock()
			lease := f.service.leases[id]
			active := lease != nil && lease.execution != nil
			f.service.mu.Unlock()
			if active {
				t.Fatal("mismatched programme activated before controller rejection")
			}
			select {
			case <-handle.closed:
				t.Fatal("rejected identity stole pending handle")
			default:
			}
		})
	}
}
