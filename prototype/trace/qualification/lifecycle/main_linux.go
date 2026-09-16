// Administrative local qualification helper. It never loads, attaches, pins,
// updates or deletes BPF state, and never enumerates global BPF IDs.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

type clockPair struct {
	Monotonic   int64 `json:"monotonicNanos"`
	Wall        int64 `json:"wallNanos"`
	Uncertainty int64 `json:"uncertaintyNanos"`
}

func readClock() (clockPair, error) {
	var before, wall, after unix.Timespec
	if unix.ClockGettime(unix.CLOCK_MONOTONIC, &before) != nil || unix.ClockGettime(unix.CLOCK_REALTIME, &wall) != nil || unix.ClockGettime(unix.CLOCK_MONOTONIC, &after) != nil {
		return clockPair{}, errOwnership
	}
	uncertainty := after.Nano() - before.Nano()
	if uncertainty < 0 || uncertainty > 5_000_000 {
		return clockPair{}, errOwnership
	}
	return clockPair{before.Nano() + uncertainty/2, wall.Nano(), uncertainty}, nil
}

func targetIDs(text string) (map[uint64]bool, error) {
	values := strings.Split(text, ",")
	if len(values) > 2 {
		return nil, errOwnership
	}
	result := map[uint64]bool{}
	for _, value := range values {
		id, err := strconv.ParseUint(value, 10, 64)
		if err != nil || id == 0 || strconv.FormatUint(id, 10) != value || result[id] {
			return nil, errOwnership
		}
		result[id] = true
	}
	return result, nil
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		if stage, ok := err.(traceFault); ok {
			fmt.Fprintln(os.Stderr, "lifecycle qualification failed:", stage)
		} else {
			fmt.Fprintln(os.Stderr, "lifecycle qualification ownership or operation failed")
		}
		os.Exit(2)
	}
}

func run(args []string) (resultErr error) {
	flags := flag.NewFlagSet("lifecycle", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	mode := flags.String("mode", "", "identify, snapshot, check, clock, signal or partial-attach")
	partialLinks := flags.Int("partial-links", 0, "accepted per-worker link inventory: 2 or 5")
	pid := flags.Int("parent-pid", 0, "fresh CRI-resolved node service PID")
	start := flags.Uint64("parent-start", 0, "previously verified node process start ticks")
	parentHash := flags.String("parent-sha256", "", "exact reviewed node executable hash")
	container := flags.String("parent-container", "", "freshly verified full CRI container ID")
	workerHash := flags.String("worker-sha256", "", "accepted worker executable hash")
	targetsText := flags.String("target-cgroups", "", "one or two verified fixture cgroup IDs")
	signal := flags.String("signal", "", "TERM or KILL")
	recipient := flags.String("recipient", "workers", "workers or parent")
	if flags.Parse(args) != nil || flags.NArg() != 0 {
		return errOwnership
	}
	encoder := json.NewEncoder(os.Stdout)
	if *mode == "clock" {
		value, err := readClock()
		if err != nil {
			return err
		}
		return encoder.Encode(value)
	}
	if *mode == "check" {
		data, err := io.ReadAll(io.LimitReader(os.Stdin, 16385))
		if err != nil {
			return errOwnership
		}
		ids, err := decodeIDs(data)
		if err != nil {
			return err
		}
		left, err := remaining(ids)
		if err != nil {
			return err
		}
		clock, err := readClock()
		if err != nil {
			return err
		}
		return encoder.Encode(struct {
			Remaining map[string]int `json:"remaining"`
			Clock     clockPair      `json:"clock"`
		}{left, clock})
	}
	if *mode != "identify" && (*start == 0 || (*mode != "snapshot" && *mode != "signal" && *mode != "partial-attach")) {
		return errOwnership
	}
	parent, err := openProcess(*pid, *parentHash, *start)
	if err != nil {
		return err
	}
	defer func() {
		if parent.close() != nil {
			resultErr = errOwnership
		}
	}()
	if _, err := parent.containerPath(*container); err != nil {
		return err
	}
	if *mode == "identify" {
		return encoder.Encode(struct {
			Start uint64 `json:"parentStart"`
		}{parent.start})
	}
	targets, err := targetIDs(*targetsText)
	if err != nil {
		return err
	}
	if *mode == "partial-attach" {
		value, err := interruptAtSyscall(parent, *workerHash, targets, *partialLinks, func() error {
			return encoder.Encode(struct {
				Ready bool `json:"ready"`
			}{true})
		})
		if encodeErr := encoder.Encode(value); encodeErr != nil {
			return encodeErr
		}
		return err
	}
	workers, excluded, err := ownedChildren(parent, *workerHash, targets)
	if err != nil {
		return err
	}
	defer func() {
		for _, worker := range workers {
			if worker.close() != nil {
				resultErr = errOwnership
			}
		}
	}()
	if *mode == "signal" {
		return signalOwned(encoder, parent, workers, excluded, *signal, *recipient)
	}
	value, err := inspectWorkers(workers, excluded)
	if err != nil || parent.check() != nil {
		return errOwnership
	}
	return encoder.Encode(value)
}

func signalOwned(encoder *json.Encoder, parent *process, workers []*process, excluded int, name, recipient string) error {
	signals := map[string]unix.Signal{"TERM": unix.SIGTERM, "KILL": unix.SIGKILL}
	signal, ok := signals[name]
	if !ok || excluded != 0 || (recipient != "workers" && recipient != "parent") {
		return errOwnership
	}
	targets := workers
	if recipient == "parent" {
		targets = []*process{parent}
	}
	if len(targets) == 0 {
		return errOwnership
	}
	clock, err := readClock()
	if err != nil {
		return err
	}
	for _, target := range targets {
		if target.check() != nil || unix.PidfdSendSignal(target.fd, signal, nil, 0) != nil {
			return errOwnership
		}
	}
	return encoder.Encode(struct {
		Signalled int       `json:"signalled"`
		Clock     clockPair `json:"clock"`
	}{len(targets), clock})
}

func decodeIDs(data []byte) (objectIDs, error) {
	if len(data) == 0 || len(data) > 16384 {
		return nil, errOwnership
	}
	var ids objectIDs
	if json.Unmarshal(data, &ids) != nil {
		return nil, errOwnership
	}
	canonical, err := json.Marshal(ids)
	if err != nil || !bytes.Equal(bytes.TrimSuffix(data, []byte("\n")), canonical) {
		return nil, errOwnership
	}
	return ids, nil
}
