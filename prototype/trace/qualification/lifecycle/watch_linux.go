package main

import (
	"context"
	"encoding/json"
	"io"
	"syscall"
	"time"
)

const maximumWatchSeconds = 1800
const maximumWatchRecordBytes = 32 << 10
const maximumWatchBytes = 16 << 20

type watchRecord struct {
	SchemaVersion        int       `json:"schemaVersion"`
	Index                int       `json:"index"`
	ElapsedNanos         int64     `json:"elapsedNanos"`
	ReadNanos            int64     `json:"readNanos"`
	State                string    `json:"state"`
	Snapshot             *snapshot `json:"snapshot,omitempty"`
	Clock                clockPair `json:"clock"`
	ObserverCPUUsec      int64     `json:"observerCPUUsec"`
	ObserverPeakRSSBytes int64     `json:"observerPeakRSSBytes"`
}

// A racing worker exit makes that observation unavailable; a changed parent
// terminates the run. Neither condition becomes an observed empty object set.
func ownedObservation(parent *process, digest string, targets map[uint64]bool) (*snapshot, error) {
	if parent.check() != nil {
		return nil, errOwnership
	}
	workers, excluded, err := ownedChildren(parent, digest, targets)
	if err != nil {
		return nil, nil
	}
	value, inspectErr := inspectWorkers(workers, excluded)
	if closeProcesses(workers) != nil {
		return nil, errOwnership
	}
	if parent.check() != nil {
		return nil, errOwnership
	}
	if inspectErr != nil {
		return nil, nil
	}
	return &value, nil
}

func watchOwned(ctx context.Context, output io.Writer, parent *process, parentHash, container, digest string, targets map[uint64]bool, seconds int) error {
	if seconds < 1 || seconds > maximumWatchSeconds {
		return errOwnership
	}
	return watchSamples(ctx, output, seconds, func() (*snapshot, error) {
		// Repeat the existing executable, lifetime and CRI membership checks.
		// Hashing cost remains in the observer's recorded CPU/RSS, never hidden.
		checked, err := openProcess(parent.pid, parentHash, parent.start)
		if err != nil {
			return nil, err
		}
		if _, err := checked.containerPath(container); err != nil {
			_ = checked.close()
			return nil, err
		}
		value, err := ownedObservation(checked, digest, targets)
		if checked.close() != nil || parent.check() != nil {
			return nil, errOwnership
		}
		return value, err
	})
}

// sample is the existing qualification ownership/census seam, injected in tests.
func watchSamples(ctx context.Context, output io.Writer, seconds int, sample func() (*snapshot, error)) error {
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
		if err := ctx.Err(); err != nil {
			return err
		}
		begin := time.Now()
		if index == 0 {
			begin = origin
		}
		value, err := sample()
		if err != nil {
			return err
		}
		clock, err := readClock()
		if err != nil {
			return err
		}
		var usage syscall.Rusage
		if syscall.Getrusage(syscall.RUSAGE_SELF, &usage) != nil {
			return errOwnership
		}
		state := "observed"
		if value == nil {
			state = "unavailable"
		}
		row := watchRecord{SchemaVersion: 1, Index: index, ElapsedNanos: begin.Sub(origin).Nanoseconds(), ReadNanos: time.Since(begin).Nanoseconds(), State: state, Snapshot: value, Clock: clock,
			ObserverCPUUsec: usage.Utime.Sec*1000000 + usage.Utime.Usec + usage.Stime.Sec*1000000 + usage.Stime.Usec, ObserverPeakRSSBytes: usage.Maxrss * 1024}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := writeWatchRecord(output, &written, row); err != nil {
			return err
		}
	}
	return nil
}

func writeWatchRecord(output io.Writer, written *int, row watchRecord) error {
	data, err := json.Marshal(row)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if len(data) > maximumWatchRecordBytes || *written > maximumWatchBytes-len(data) {
		return errOwnership
	}
	n, err := output.Write(data)
	*written += n
	if err != nil {
		return err
	}
	if n != len(data) {
		return io.ErrShortWrite
	}
	return nil
}
