// Command replica-workload provides an inert process and a bounded allocation
// mode. Exec the same binary inside one replica to vary charge without changing
// the template, resource limits, labels or immutable image of any peer.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"
)

type options struct {
	mib      int
	duration time.Duration
}

func parse(args []string) (options, error) {
	var result options
	flags := flag.NewFlagSet("replica-workload", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.IntVar(&result.mib, "hold-mib", 0, "additional resident memory, at most 64 MiB")
	flags.DurationVar(&result.duration, "duration", 12*time.Minute, "allocation lifetime, at most 15 minutes")
	if flags.Parse(args) != nil || flags.NArg() != 0 || result.mib < 0 || result.mib > 64 || result.duration <= 0 || result.duration > 15*time.Minute {
		return options{}, errors.New("use hold-mib between 0 and 64 and duration greater than zero up to 15m")
	}
	return result, nil
}

func run(ctx context.Context, o options, out io.Writer) {
	if o.mib == 0 {
		<-ctx.Done()
		return
	}
	held := make([]byte, o.mib<<20)
	for i := 0; i < len(held); i += 4096 {
		held[i] = 1
	}
	fmt.Fprintf(out, "holding %d bytes for %s\n", len(held), o.duration)
	timer := time.NewTimer(o.duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
	runtime.KeepAlive(held)
}

func main() {
	options, err := parse(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	run(ctx, options, os.Stdout)
}
