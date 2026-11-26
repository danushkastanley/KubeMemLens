package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

func main() {
	flags := flag.NewFlagSet("verifier-observer", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	path := flags.String("config", "", "private frozen configuration")
	ack := flags.Bool("acknowledge-owned-node", false, "explicit owned-node verifier observation")
	if flags.Parse(os.Args[1:]) != nil || flags.NArg() != 0 || !*ack {
		fmt.Fprintln(os.Stderr, "invalid verifier observer arguments")
		os.Exit(2)
	}
	if unix.Setrlimit(unix.RLIMIT_CORE, &unix.Rlimit{}) != nil {
		fmt.Fprintln(os.Stderr, "verifier core-dump guard failed")
		os.Exit(2)
	}
	cfg, err := load(*path)
	if err != nil {
		fmt.Fprintln(os.Stderr, errInput)
		os.Exit(2)
	}
	runtime.GOMAXPROCS(2)
	// A disconnected evidence reader must return through cleanup, not terminate
	// this process via the special SIGPIPE handling of stdout writes.
	signal.Ignore(syscall.SIGPIPE)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	deadline := time.AfterFunc(time.Duration(cfg.Seconds+10)*time.Second, func() { os.Exit(2) })
	defer deadline.Stop()
	if err := run(ctx, cfg, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "verifier observation incomplete:", err)
		os.Exit(1)
	}
}
