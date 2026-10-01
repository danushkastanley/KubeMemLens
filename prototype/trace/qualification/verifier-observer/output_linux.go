package main

import (
	"encoding/json"
	"errors"
	"io"
	"syscall"

	"github.com/danushkastanley/kube-memlens/prototype/trace/qualification/verifier"
	"golang.org/x/sys/unix"
)

type binding struct {
	Owner       string            `json:"owner"`
	BTF         string            `json:"btfSHA256"`
	Executable  string            `json:"anchorSHA256"`
	CgroupInode uint64            `json:"cgroupInode"`
	Formats     map[string]string `json:"formatSHA256"`
}
type observation struct {
	SchemaVersion    int                            `json:"schemaVersion"`
	Index            int                            `json:"index"`
	Started          uint64                         `json:"startedNanos"`
	Cutoff           uint64                         `json:"cutoffNanos"`
	ReadStarted      uint64                         `json:"readStartedNanos"`
	ReadEnded        uint64                         `json:"readEndedNanos"`
	CPUUsec          int64                          `json:"observerCPUUsec"`
	RSSBytes         int64                          `json:"observerPeakRSSBytes"`
	StartupDiscarded uint64                         `json:"startupDiscarded"`
	Perf             verifier.PerfCounters          `json:"perf"`
	Observation      verifier.Totals                `json:"observation"`
	Probes           map[string]verifier.ProbeCount `json:"probeCounts"`
	Binding          binding                        `json:"binding"`
	Closed           bool                           `json:"captureClosed"`
	CloseStarted     uint64                         `json:"closeStartedNanos"`
	CloseEnded       uint64                         `json:"closeEndedNanos"`
}

func clock() (uint64, error) {
	var t unix.Timespec
	if unix.ClockGettime(unix.CLOCK_MONOTONIC, &t) != nil || t.Nano() <= 0 {
		return 0, verifier.ErrObservation
	}
	return uint64(t.Nano()), nil
}

func resources() (int64, int64, error) {
	var usage syscall.Rusage
	if syscall.Getrusage(syscall.RUSAGE_SELF, &usage) != nil {
		return 0, 0, verifier.ErrObservation
	}
	cpu := usage.Utime.Sec*1000000 + usage.Utime.Usec + usage.Stime.Sec*1000000 + usage.Stime.Usec
	if cpu < 0 || usage.Maxrss <= 0 || usage.Maxrss > 1<<40 {
		return 0, 0, verifier.ErrObservation
	}
	return cpu, usage.Maxrss * 1024, nil
}

func writeRow(out io.Writer, row observation, written *int) error {
	raw, err := json.Marshal(row)
	if err != nil {
		return verifier.ErrObservation
	}
	raw = append(raw, '\n')
	if len(raw) > 8192 || *written > 16<<20-len(raw) {
		return errors.New("verifier evidence exceeds bound")
	}
	n, err := out.Write(raw)
	*written += n
	if err != nil || n != len(raw) {
		return errors.New("verifier evidence write failed")
	}
	return nil
}
