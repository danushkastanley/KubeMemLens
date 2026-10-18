package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/danushkastanley/kube-memlens/prototype/trace/qualification/scheduler"
	"golang.org/x/sys/unix"
)

const reorderNanos = uint64(100 * time.Millisecond)

func clock() (uint64, error) {
	var ts unix.Timespec
	if unix.ClockGettime(unix.CLOCK_MONOTONIC, &ts) != nil || ts.Nano() <= 0 {
		return 0, scheduler.ErrObservation
	}
	return uint64(ts.Nano()), nil
}

func run(ctx context.Context, cfg configuration, out io.Writer, completion *os.File) (result error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := cfg.identities(); err != nil {
		return err
	}
	anchor, err := bindAnchor(cfg.Anchor)
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, anchor.close()) }()
	formats, err := cfg.formats()
	if err != nil {
		return err
	}
	cpus, err := cpuList(cfg.OnlineCPUs)
	if err != nil {
		return err
	}
	capture, err := scheduler.OpenCapture(cpus, formats)
	if err != nil {
		return fmt.Errorf("capture setup: %w", err)
	}
	defer func() { result = errors.Join(result, capture.Close()) }()
	merge, err := scheduler.NewMerge(cpus, 65536)
	if err != nil {
		return err
	}
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	written := 0
	for index := 0; index <= cfg.Seconds; {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
		if err := anchor.alive(); err != nil {
			return err
		}
		begin, err := clock()
		if err != nil {
			return err
		}
		if err := capture.Drain(merge.Push); err != nil {
			return fmt.Errorf("record drain: %w", err)
		}
		cutoff := capture.Started() + uint64(index)*uint64(time.Second)
		if begin < capture.Started()+reorderNanos {
			continue
		}
		if begin < cutoff+reorderNanos {
			if _, err := merge.Advance(begin - reorderNanos); err != nil {
				return fmt.Errorf("event ordering: %w", err)
			}
			continue
		}
		if begin-cutoff-reorderNanos > uint64(250*time.Millisecond) {
			return errors.New("scheduler sample deadline missed")
		}
		coverage, err := capture.CheckCounters()
		if err != nil {
			return fmt.Errorf("perf coverage: %w", err)
		}
		stats, err := merge.Advance(cutoff)
		if err != nil {
			return fmt.Errorf("event ordering: %w", err)
		}
		if err := cfg.identities(); err != nil {
			return err
		}
		var usage syscall.Rusage
		if syscall.Getrusage(syscall.RUSAGE_SELF, &usage) != nil {
			return scheduler.ErrObservation
		}
		end, err := clock()
		if err != nil {
			return err
		}
		if end < begin || end-begin > uint64(50*time.Millisecond) {
			return errors.New("scheduler observation read exceeded bound")
		}
		row := struct {
			SchemaVersion    int                `json:"schemaVersion"`
			Index            int                `json:"index"`
			Started          uint64             `json:"startedNanos"`
			Cutoff           uint64             `json:"cutoffNanos"`
			ReadStarted      uint64             `json:"readStartedNanos"`
			ReadEnded        uint64             `json:"readEndedNanos"`
			CPUUsec          int64              `json:"observerCPUUsec"`
			RSSBytes         int64              `json:"observerPeakRSSBytes"`
			StartupDiscarded uint64             `json:"startupDiscarded"`
			Perf             scheduler.Counters `json:"perf"`
			Observation      scheduler.Snapshot `json:"observation"`
		}{2, index, capture.Started(), cutoff, begin, end,
			usage.Utime.Sec*1000000 + usage.Utime.Usec + usage.Stime.Sec*1000000 + usage.Stime.Usec,
			usage.Maxrss * 1024, capture.StartupDiscarded(), coverage, stats}
		data, err := json.Marshal(row)
		if err != nil {
			return scheduler.ErrObservation
		}
		data = append(data, '\n')
		if len(data) > 8192 || written > 16<<20-len(data) {
			return scheduler.ErrObservation
		}
		n, err := out.Write(data)
		written += n
		if err != nil || n != len(data) {
			return errors.New("scheduler evidence write failed")
		}
		index++
	}
	if completion != nil {
		if err := capture.Stop(); err != nil {
			return err
		}
		if err := awaitCompletion(ctx, completion); err != nil {
			return err
		}
	}
	return anchor.alive()
}

func main() {
	flags := flag.NewFlagSet("scheduler-observer", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	path := flags.String("config", "", "private frozen configuration")
	ack := flags.Bool("acknowledge-owned-node", false, "explicit owned-node perf capture")
	completionSignal := flags.String("completion-signal", "", "stdin-eof waits for the controller before closing perf descriptors")
	if flags.Parse(os.Args[1:]) != nil || flags.NArg() != 0 || !*ack || (*completionSignal != "" && *completionSignal != "stdin-eof") {
		fmt.Fprintln(os.Stderr, "invalid scheduler observer arguments")
		os.Exit(2)
	}
	cfg, err := load(*path)
	if err != nil {
		fmt.Fprintln(os.Stderr, errInput)
		os.Exit(2)
	}
	// Raw kernel records stay in volatile rings: never allow this process to dump
	// their contents to a core file. This changes only this process's resource limit.
	if unix.Setrlimit(unix.RLIMIT_CORE, &unix.Rlimit{}) != nil {
		fmt.Fprintln(os.Stderr, "scheduler core-dump guard failed")
		os.Exit(2)
	}
	runtime.GOMAXPROCS(2)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	deadline := time.AfterFunc(time.Duration(cfg.Seconds+10)*time.Second, func() { os.Exit(2) })
	defer deadline.Stop()
	var completion *os.File
	if *completionSignal == "stdin-eof" {
		completion = os.Stdin
	}
	if err := run(ctx, cfg, os.Stdout, completion); err != nil {
		fmt.Fprintln(os.Stderr, "scheduler observation incomplete:", err)
		os.Exit(1)
	}
}
