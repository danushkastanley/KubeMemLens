package sdk

import "context"

type readFlusher interface {
	Flush() error
}

// interruptRead wakes an idle reader on cancellation without periodic polling.
// The returned finaliser must be called once before closing reader resources;
// it joins an in-flight callback and reports a failed wake-up.
func interruptRead(ctx context.Context, reader readFlusher) func() error {
	finished := make(chan error, 1)
	stop := context.AfterFunc(ctx, func() { finished <- reader.Flush() })
	return func() error {
		if stop() {
			return nil
		}
		return <-finished
	}
}
