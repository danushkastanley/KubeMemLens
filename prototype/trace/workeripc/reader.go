package workeripc

import (
	"bufio"
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
	// Coalesce pipe reads within one maximum-size framed message. Validation,
	// cumulative limits and the required terminal EOF use this same reader.
	stream := bufio.NewReaderSize(in, MaxMessageBytes+4)
	files := fileBatch{output: output}
	defer files.clear()
	fail := func(err error) (trace.Result, error) {
		if files.flush() != nil {
			return trace.Result{}, ErrOutput
		}
		return trace.Result{}, err
	}
	ready := false
	var events, bytes uint64
	for {
		var message responseWire
		size, err := receive(stream, &message)
		if err != nil || message.validate(request) != nil {
			return fail(ErrProtocol)
		}
		switch message.Type {
		case "ready":
			if files.flush() != nil {
				return trace.Result{}, ErrOutput
			}
			if ready {
				return trace.Result{}, ErrProtocol
			}
			ready = true
			onReady()
		case "result":
			if files.flush() != nil {
				return trace.Result{}, ErrOutput
			}
			if (!ready && (message.Result.Termination != trace.EngineFailed || !message.Result.StartedAt.IsZero())) || !message.Result.covers(events) || end(stream) != nil {
				return trace.Result{}, ErrProtocol
			}
			return message.Result.result(request)
		default:
			bounds := request.Specification.Bounds()
			if !ready || events >= bounds.Events || uint64(size) > bounds.OutputBytes-bytes {
				return fail(ErrProtocol)
			}
			events++
			bytes += uint64(size)
			if message.File != nil {
				event, err := fileObservation(message.File, request.Specification)
				if err != nil {
					return fail(ErrProtocol)
				}
				if files.add(event, stream) != nil {
					return trace.Result{}, ErrOutput
				}
				continue
			}
			if err := forward(message, request.Specification, output); err != nil {
				return trace.Result{}, ErrOutput
			}
		}
	}
}

func forward(message responseWire, spec trace.Specification, output trace.Output) error {
	if message.OOM != nil {
		o := message.OOM
		command, err := trace.NewSensitiveText(o.Command, 16)
		if err != nil {
			return ErrProtocol
		}
		return output.OOMDecision(trace.OOMDecision{ObservedAt: o.ObservedAt, Scope: o.Scope, VictimPID: o.VictimPID, Command: command})
	}
	c := message.Cache
	return output.CacheActivity(trace.CacheActivity{ObservedAt: c.ObservedAt, Operation: c.Operation, Pages: c.Pages})
}
