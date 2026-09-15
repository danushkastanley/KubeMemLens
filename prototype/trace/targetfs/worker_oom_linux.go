package targetfs

import (
	"context"
	"errors"
	"os"

	"github.com/danushkastanley/kube-memlens/internal/trace"
	admission "github.com/danushkastanley/kube-memlens/internal/traceadmission"
	"github.com/danushkastanley/kube-memlens/prototype/trace/oomtrace"
)

// ReadWorkerOOMSample reads only the fixed OOM evidence files. Unavailable files
// remain nil; target loss cancels the entire sample instead of changing cgroups.
func ReadWorkerOOMSample(ctx context.Context, file *os.File, target trace.TargetIdentity) (oomtrace.RawSample, error) {
	return readOOMFiles(ctx, func(ctx context.Context, name string) ([]byte, error) {
		return readWorkerFile(ctx, file, target, name)
	})
}

func readOOMFiles(ctx context.Context, read func(context.Context, string) ([]byte, error)) (oomtrace.RawSample, error) {
	var raw oomtrace.RawSample
	files := []struct {
		name string
		data *[]byte
	}{{"memory.events.local", &raw.Local}, {"memory.events", &raw.Hierarchical}, {"memory.current", &raw.Current}, {"memory.max", &raw.Limit}, {"memory.pressure", &raw.Pressure}}
	present := 0
	for _, file := range files {
		if ctx.Err() != nil {
			return oomtrace.RawSample{}, ctx.Err()
		}
		data, err := read(ctx, file.name)
		if errors.Is(err, admission.ErrTargetChanged) {
			return oomtrace.RawSample{}, err
		}
		if err != nil {
			if !errors.Is(err, admission.ErrUnavailable) {
				return oomtrace.RawSample{}, err
			}
			continue
		}
		if len(data) != 0 {
			*file.data = data
			present++
		}
	}
	if present == 0 {
		return oomtrace.RawSample{}, admission.ErrUnavailable
	}
	if err := ctx.Err(); err != nil {
		return oomtrace.RawSample{}, err
	}
	return raw, nil
}
