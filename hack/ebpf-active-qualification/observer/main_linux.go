package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

type clockPair struct {
	Monotonic   int64 `json:"monotonicNanos"`
	Wall        int64 `json:"wallNanos"`
	Uncertainty int64 `json:"uncertaintyNanos"`
}

func readClock() (clockPair, error) {
	var a, b, c unix.Timespec
	if unix.ClockGettime(unix.CLOCK_MONOTONIC, &a) != nil || unix.ClockGettime(unix.CLOCK_REALTIME, &b) != nil || unix.ClockGettime(unix.CLOCK_MONOTONIC, &c) != nil {
		return clockPair{}, errObservation
	}
	d := c.Nano() - a.Nano()
	if d < 0 || d > 5000000 {
		return clockPair{}, errObservation
	}
	return clockPair{a.Nano() + d/2, b.Nano(), d}, nil
}

type observation struct {
	SchemaVersion        int                  `json:"schemaVersion"`
	Index                int                  `json:"index"`
	ElapsedNanos         int64                `json:"elapsedNanos"`
	ReadNanos            int64                `json:"readNanos"`
	Clock                clockPair            `json:"clock"`
	Agent                map[string]uint64    `json:"agent"`
	Collector            collectorObservation `json:"collector"`
	ObserverCPUUsec      int64                `json:"observerCPUUsec"`
	ObserverPeakRSSBytes int64                `json:"observerPeakRSSBytes"`
}

func usage() (int64, int64, error) {
	var self, children syscall.Rusage
	if syscall.Getrusage(syscall.RUSAGE_SELF, &self) != nil || syscall.Getrusage(syscall.RUSAGE_CHILDREN, &children) != nil {
		return 0, 0, errObservation
	}
	cpu := int64(0)
	for _, v := range []syscall.Rusage{self, children} {
		cpu += v.Utime.Sec*1000000 + v.Utime.Usec + v.Stime.Sec*1000000 + v.Stime.Usec
	}
	return cpu, (self.Maxrss + children.Maxrss) * 1024, nil
}
func sampleSeries(ctx context.Context, out io.Writer, seconds int, sample func() (map[string]uint64, collectorObservation, error)) error {
	if seconds < 1 || seconds > 1800 {
		return errObservation
	}
	origin := time.Now()
	written := 0
	for index := 0; index <= seconds; index++ {
		timer := time.NewTimer(time.Until(origin.Add(time.Duration(index) * time.Second)))
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		begin := time.Now()
		if index == 0 {
			begin = origin
		}
		agent, collector, err := sample()
		if err != nil {
			return err
		}
		clock, err := readClock()
		if err != nil {
			return err
		}
		cpu, rss, err := usage()
		if err != nil {
			return err
		}
		row := observation{1, index, begin.Sub(origin).Nanoseconds(), time.Since(begin).Nanoseconds(), clock, agent, collector, cpu, rss}
		data, err := json.Marshal(row)
		if err != nil {
			return err
		}
		data = append(data, '\n')
		if len(data) > 8192 || written > 16<<20-len(data) {
			return errObservation
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		n, err := out.Write(data)
		written += n
		if err != nil {
			return err
		}
		if n != len(data) {
			return io.ErrShortWrite
		}
	}
	return nil
}
func run(ctx context.Context, cfg config, out io.Writer) error {
	if !sameBoot(cfg.BootID) {
		return errObservation
	}
	agent, err := openBound(cfg.AgentPID, cfg.AgentStart, cfg.AgentSHA256, cfg.AgentContainer)
	if err != nil {
		return err
	}
	defer agent.close()
	collector, err := openBound(cfg.CollectorPID, cfg.CollectorStart, cfg.CollectorSHA256, cfg.CollectorContainer)
	if err != nil {
		return err
	}
	defer collector.close()
	client, err := collectorClient(cfg.Server, cfg.Token, cfg.CAPEM)
	if err != nil {
		return err
	}
	defer client.CloseIdleConnections()
	return sampleSeries(ctx, out, cfg.Seconds, func() (map[string]uint64, collectorObservation, error) {
		if !sameBoot(cfg.BootID) || agent.verify() != nil || collector.verify() != nil {
			return nil, collectorObservation{}, errObservation
		}
		a, err := readAgent(ctx, agent)
		if err != nil {
			return nil, collectorObservation{}, err
		}
		c, err := readCollector(ctx, client, cfg.Server, cfg.Token)
		if err != nil || agent.alive() != nil || collector.alive() != nil || !sameBoot(cfg.BootID) {
			return nil, collectorObservation{}, errObservation
		}
		return a, c, nil
	})
}
func main() {
	flags := flag.NewFlagSet("standard-observer", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	path := flags.String("config", "", "private local measurement configuration")
	child := flags.Bool("agent-scrape", false, "internal fixed-loopback reader")
	if flags.Parse(os.Args[1:]) != nil || flags.NArg() != 0 || (*child && *path != "") {
		fmt.Fprintln(os.Stderr, "invalid metrics observer arguments")
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	if *child {
		deadline := time.AfterFunc(4*time.Second, func() { os.Exit(2) })
		defer deadline.Stop()
		if scrapeAgent(ctx, os.Stdout) != nil {
			os.Exit(1)
		}
		return
	}
	cfg, err := loadConfig(*path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "invalid private metrics configuration")
		os.Exit(2)
	}
	deadline := time.AfterFunc(time.Duration(cfg.Seconds+10)*time.Second, func() { os.Exit(2) })
	defer deadline.Stop()
	if run(ctx, cfg, os.Stdout) != nil {
		fmt.Fprintln(os.Stderr, "metrics observation incomplete; retain numeric records")
		os.Exit(1)
	}
}
