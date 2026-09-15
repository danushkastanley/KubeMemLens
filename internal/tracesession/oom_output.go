package tracesession

import (
	"github.com/danushkastanley/kube-memlens/internal/trace"
	"github.com/danushkastanley/kube-memlens/internal/traceframe"
)

func (o *output) aggregateOOM(event trace.OOMDecision) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if err := o.aggregateWindow(event.ObservedAt); err != nil {
		return err
	}
	frame, err := traceframe.NewOOMVersion(event, o.spec, traceframe.OOMVersion)
	if err != nil {
		return o.rejectAggregate(trace.EngineFailed)
	}
	data, err := traceframe.Encode(frame)
	if err != nil {
		return o.rejectAggregate(trace.EngineFailed)
	}
	if o.bytes+uint64(len(data))+traceframe.OOMTerminalReserve > o.spec.Bounds().OutputBytes {
		return o.rejectAggregate(trace.OutputLimit)
	}
	if err := o.write(data); err != nil {
		o.rejected++
		return err
	}
	o.events++
	return o.finishAggregate(o.aggregates.OOM(event))
}
