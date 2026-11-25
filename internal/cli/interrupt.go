package cli

import (
	"context"
	"os"
	"os/signal"
	"syscall"
)

// interruptContext returns a context that is cancelled by the first interrupt
// or termination signal. The signals then go back to their default action, so
// a second Ctrl+C ends a command that does not stop.
func interruptContext(parent context.Context) (context.Context, context.CancelFunc) {
	ctx, stop := signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
	context.AfterFunc(ctx, stop)
	return ctx, stop
}
