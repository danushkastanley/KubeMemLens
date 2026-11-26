package targetfs

import (
	"context"
	"errors"
	"reflect"
	"testing"

	admission "github.com/danushkastanley/kube-memlens/internal/traceadmission"
)

func TestOOMReadSetAndUnavailableFields(t *testing.T) {
	var names []string
	raw, err := readOOMFiles(context.Background(), func(_ context.Context, name string) ([]byte, error) {
		names = append(names, name)
		if name == "memory.pressure" {
			return nil, admission.ErrUnavailable
		}
		return []byte("0\n"), nil
	})
	want := []string{"memory.events.local", "memory.events", "memory.current", "memory.max", "memory.pressure"}
	if err != nil || !reflect.DeepEqual(names, want) || raw.Pressure != nil || raw.Local == nil || raw.Hierarchical == nil {
		t.Fatal("OOM evidence read set or missing-field semantics changed")
	}
}

func TestOOMTargetLossDiscardsEarlierFiles(t *testing.T) {
	calls := 0
	raw, err := readOOMFiles(context.Background(), func(context.Context, string) ([]byte, error) {
		calls++
		if calls == 2 {
			return nil, admission.ErrTargetChanged
		}
		return []byte("0\n"), nil
	})
	if !errors.Is(err, admission.ErrTargetChanged) || calls != 2 || raw.Local != nil {
		t.Fatal("target replacement retained or continued sampling")
	}
	_, err = readOOMFiles(context.Background(), func(context.Context, string) ([]byte, error) { return nil, admission.ErrUnavailable })
	if !errors.Is(err, admission.ErrUnavailable) {
		t.Fatal("entirely unavailable sample reported success")
	}
}
