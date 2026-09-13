package trace

import (
	"context"
	"errors"
	"testing"
)

type memoryAdapter struct {
	runs int
	spec Specification
}

func (a *memoryAdapter) Run(_ context.Context, spec Specification, output Output) (Result, error) {
	a.runs++
	a.spec = spec
	if err := output.FileActivity(FileActivity{Operation: FileRead}); err != nil {
		return Result{Version: ContractVersion, Termination: EngineFailed, Incomplete: true}, err
	}
	return Result{Version: ContractVersion, Termination: Expired}, nil
}

type memoryOutput struct{ err error }

func (o memoryOutput) FileActivity(FileActivity) error   { return o.err }
func (o memoryOutput) CacheActivity(CacheActivity) error { return o.err }
func (o memoryOutput) OOMDecision(OOMDecision) error     { return o.err }

func TestEngineRejectsInvalidWorkBeforeAdapterInvocation(t *testing.T) {
	adapter := &memoryAdapter{}
	engine, err := NewEngine(adapter)
	if err != nil {
		t.Fatal(err)
	}
	spec, err := NewSpecification(Files, targetFixture(), OmitPaths, DefaultBounds())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := engine.Run(ctx, spec, memoryOutput{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled work accepted: %v", err)
	}
	if _, err := engine.Run(context.Background(), Specification{}, memoryOutput{}); err == nil {
		t.Fatal("zero specification reached engine")
	}
	if _, err := engine.Run(context.Background(), spec, nil); err == nil {
		t.Fatal("work with no consumer accepted")
	}
	if adapter.runs != 0 {
		t.Fatal("rejected work invoked the adapter")
	}
	if _, err := NewEngine(nil); err == nil {
		t.Fatal("missing adapter accepted")
	}
	var absent *Engine
	if _, err := absent.Run(context.Background(), spec, memoryOutput{}); err == nil {
		t.Fatal("missing engine accepted")
	}
}

func TestEnginePreservesSpecificationAndPropagatesOutputFailure(t *testing.T) {
	adapter := &memoryAdapter{}
	engine, err := NewEngine(adapter)
	if err != nil {
		t.Fatal(err)
	}
	spec, err := NewSpecification(Files, targetFixture(), OmitPaths, DefaultBounds())
	if err != nil {
		t.Fatal(err)
	}
	result, err := engine.Run(context.Background(), spec, memoryOutput{})
	if err != nil || result.Version != ContractVersion || adapter.spec != spec || adapter.runs != 1 {
		t.Fatalf("engine changed the admitted contract: %v", err)
	}
	outputErr := errors.New("consumer disconnected")
	result, err = engine.Run(context.Background(), spec, memoryOutput{err: outputErr})
	if !errors.Is(err, outputErr) || !result.Incomplete || adapter.runs != 2 {
		t.Fatal("output failure became successful trace output")
	}
}
