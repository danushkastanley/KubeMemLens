// Command memory-history-workload keeps an inert, bounded fixture container alive.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	delay, err := exitDelay(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	wait(ctx, delay)
}

func exitDelay(args []string) (time.Duration, error) {
	flags := flag.NewFlagSet("history-workload", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	delay := flags.Duration("exit-after", 0, "optional bounded lifetime for restart qualification")
	if flags.Parse(args) != nil || flags.NArg() != 0 || *delay < 0 || *delay > 2*time.Minute {
		return 0, errors.New("fixture exit-after must be between 0 and 2m, without positional arguments")
	}
	return *delay, nil
}

func wait(ctx context.Context, delay time.Duration) {
	var elapsed <-chan time.Time
	if delay > 0 {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		elapsed = timer.C
	}
	select {
	case <-ctx.Done():
	case <-elapsed:
	}
}
