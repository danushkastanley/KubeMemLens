//go:build linux

package main

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTaskSchedulingIsNotAZeroOnMalformedInput(t *testing.T) {
	for _, value := range []string{"", "1 2", "1 2 3 4", "1 -1 3", "1 18446744073709551616 3"} {
		if _, err := parseTaskScheduling([]byte(value)); err == nil {
			t.Fatalf("accepted %q", value)
		}
	}
	got, err := parseTaskScheduling([]byte("123 456 7\n"))
	if err != nil || got != (totals{123, 456, 7}) {
		t.Fatalf("unexpected task counters %+v %v", got, err)
	}
}
func TestNodeSchedulingVersionsAndCPUFields(t *testing.T) {
	for _, version := range []string{"15", "16", "17"} {
		s, err := parseNodeScheduling([]byte("version " + version + "\ntimestamp 42\ncpu0 1 0 2 3 4 5 10 20 2\ndomain0 ignored\ncpu2 1 0 2 3 4 5 30 40 3\n"))
		if err != nil || s.CPUs != 2 || s.Totals() != (totals{40, 60, 5}) {
			t.Fatalf("version %s: %+v %v", version, s, err)
		}
	}
	for _, text := range []string{
		"version 18\ntimestamp 1\ncpu0 0 0 0 0 0 0 1 2 3",
		"version 17\ncpu0 0 0 0 0 0 0 1 2 3",
		"version 17\ntimestamp 1\ncpu0 0 0 0 0 0 0 1 2",
		"version 17\ntimestamp 1\ncpu0 0 0 0 0 0 0 1 2 3\ncpu0 0 0 0 0 0 0 1 2 3",
		"version 17\ntimestamp 1\ncpuX 0 0 0 0 0 0 1 2 3",
		"version 17\ntimestamp 1\ncpu0 0 0 0 0 0 0 18446744073709551615 2 3\ncpu1 0 0 0 0 0 0 1 2 3",
	} {
		if _, err := parseNodeScheduling([]byte(text)); err == nil {
			t.Fatalf("accepted malformed scheduler counters: %s", text)
		}
	}
}
func TestUnavailableSchedulingOmitsAllNumericEvidence(t *testing.T) {
	p := filepath.Join(t.TempDir(), "missing")
	s := nodeScheduling(p, p+"-enabled")
	if s.State != "unsupported" {
		t.Fatal(s)
	}
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "Nanos") || strings.Contains(string(data), "timeslices") {
		t.Fatal("missing counters became numbers")
	}
	if err := os.WriteFile(p, []byte("invalid"), 0600); err != nil {
		t.Fatal(err)
	}
	if s = nodeScheduling(p, p+"-enabled"); s.State != "unavailable" || s.RuntimeNanos != nil {
		t.Fatal("invalid source was accepted")
	}
}
func TestSchedulerCounterOverflowIsRejected(t *testing.T) {
	for _, a := range []totals{{math.MaxUint64, 0, 0}, {0, math.MaxUint64, 0}, {0, 0, math.MaxUint64}} {
		if addTotals(&a, totals{1, 1, 1}) == nil {
			t.Fatal("overflow accepted")
		}
	}
}

func TestDisabledSchedulingIsNotAnObservedZero(t *testing.T) {
	path := filepath.Join(t.TempDir(), "schedstat")
	if err := os.WriteFile(path, []byte("version 17\ntimestamp 1\ncpu0 0 0 0 0 0 0 0 0 0\n"), 0600); err != nil {
		t.Fatal(err)
	}
	flag := path + "-enabled"
	if err := os.WriteFile(flag, []byte("0\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if s := nodeScheduling(path, flag); s.State != "disabled" || s.RuntimeNanos != nil || s.Enabled == nil || *s.Enabled {
		t.Fatalf("disabled source misrepresented %+v", s)
	}
	if err := os.WriteFile(flag, []byte("1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if s := nodeScheduling(path, flag); s.State != "observed" || s.RuntimeNanos == nil || s.Enabled == nil || !*s.Enabled {
		t.Fatalf("enabled source missing %+v", s)
	}
}
