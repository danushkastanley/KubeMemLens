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

	p "github.com/danushkastanley/kube-memlens/internal/tracepreflight"
	"github.com/danushkastanley/kube-memlens/prototype/trace/probe"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
}

func run(ctx context.Context, args []string, out, errOut io.Writer) error {
	if len(args) == 0 || args[0] != "doctor" && args[0] != "_probe" {
		return errors.New("usage: memlens-trace doctor [--json] [--bundle DIRECTORY] [--timeout 10s]")
	}
	flags := flag.NewFlagSet("doctor", flag.ContinueOnError)
	flags.SetOutput(errOut)
	bundle := flags.String("bundle", "/opt/memlens-trace/reference", "directory containing frozen reference manifests")
	jsonOutput := flags.Bool("json", false, "write the bounded preflight report as JSON")
	timeout := flags.Duration("timeout", 10*time.Second, "preflight timeout, at most 15 seconds")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 || *timeout <= 0 || *timeout > 15*time.Second {
		return errors.New("preflight requires no positional arguments and a timeout at most 15 seconds")
	}
	if args[0] == "_probe" {
		checker, err := p.New(probe.New(*bundle), *timeout)
		if err != nil {
			return err
		}
		return writeJSON(out, checker.Run(ctx))
	}
	report := supervise(ctx, *bundle, *timeout)
	if *jsonOutput {
		if err := writeJSON(out, report); err != nil {
			return err
		}
	} else {
		if err := writeText(out, report); err != nil {
			return err
		}
	}
	if report.State != p.Supported {
		return errors.New("preflight is incomplete or unsupported; inspect the reported reasons")
	}
	return nil
}

func writeJSON(out io.Writer, r p.Report) error {
	data, err := p.Encode(r)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	n, err := out.Write(data)
	if err != nil {
		return err
	}
	if n != len(data) {
		return io.ErrShortWrite
	}
	return nil
}

func writeText(out io.Writer, r p.Report) error {
	if _, err := p.Encode(r); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "Engine baseline preflight: %s\nIncident tracing remains unavailable pending programme approval.\n", r.State); err != nil {
		return err
	}
	for _, c := range r.Checks {
		if _, err := fmt.Fprintf(out, "%-26s %-12s %s", c.ID, c.State, c.Reason); err != nil {
			return err
		}
		if c.Value != "" {
			if _, err := fmt.Fprintf(out, " (%s)", c.Value); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintln(out); err != nil {
			return err
		}
	}
	return nil
}
