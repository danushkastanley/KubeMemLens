package workeripc

import (
	"io"

	"github.com/danushkastanley/kube-memlens/internal/trace"
)

// ReadStream validates every private message before delivering it. The caller
// owns pipe deadlines, cancellation, termination and process reaping. onReady
// ends only the startup timer; the overall execution deadline must remain active.
func ReadStream(in io.Reader, request Request, output trace.Output, onReady func()) (trace.Result, error) {
	if in == nil || output == nil || onReady == nil || request.Validate() != nil {
		return trace.Result{}, ErrProtocol
	}
	ready := false
	var events, bytes uint64
	for {
		var message responseWire
		size, err := receive(in, &message)
		if err != nil || message.validate(request) != nil {
			return trace.Result{}, ErrProtocol
		}
		switch message.Type {
		case "ready":
			if ready {
				return trace.Result{}, ErrProtocol
			}
			ready = true
			onReady()
		case "result":
			if (!ready && (message.Result.Termination != trace.EngineFailed || !message.Result.StartedAt.IsZero())) || !message.Result.covers(events) || end(in) != nil {
				return trace.Result{}, ErrProtocol
			}
			return message.Result.result(request)
		default:
			bounds := request.Specification.Bounds()
			if !ready || events >= bounds.Events || uint64(size) > bounds.OutputBytes-bytes {
				return trace.Result{}, ErrProtocol
			}
			events++
			bytes += uint64(size)
			if err := forward(message, request.Specification, output); err != nil {
				return trace.Result{}, ErrOutput
			}
		}
	}
}

func forward(message responseWire, spec trace.Specification, output trace.Output) error {
	if message.File != nil {
		f := message.File
		path, err := trace.NewSensitiveText(f.Path, spec.Bounds().PathBytes)
		if err != nil {
			return ErrProtocol
		}
		return output.FileActivity(trace.FileActivity{ObservedAt: f.ObservedAt, Operation: f.Operation, RequestedBytes: &f.Requested, CompletedBytes: &f.Completed, Path: path})
	}
	c := message.Cache
	return output.CacheActivity(trace.CacheActivity{ObservedAt: c.ObservedAt, Operation: c.Operation, Pages: c.Pages})
}
