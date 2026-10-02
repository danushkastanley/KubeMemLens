package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"testing"

	"golang.org/x/sys/unix"
)

func TestPIDFDRejectsChangedLifetimeAndExitedProcess(t *testing.T) {
	cmd := exec.Command("/bin/sh", "-c", "read value")
	input, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Wait()
	defer input.Close()
	fd, err := unix.PidfdOpen(cmd.Process.Pid, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(fd)
	start, err := startTime(cmd.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	p := &boundProcess{pid: cmd.Process.Pid, fd: fd, start: start}
	if p.alive() != nil {
		t.Fatal("live process rejected")
	}
	p.start++
	if p.alive() == nil {
		t.Fatal("reused lifetime accepted")
	}
	p.start--
	input.Close()
	_ = cmd.Wait()
	if p.alive() == nil {
		t.Fatal("exited process accepted")
	}
	if _, err := openBound(os.Getpid(), 1, "invalid", "invalid"); err == nil {
		t.Fatal("invalid binding accepted")
	}
}

type shortWriter struct{}

func (shortWriter) Write(data []byte) (int, error) { return len(data) - 1, nil }
func sampleFixture() (agentObservation, collectorObservation, error) {
	a, err := parseAgent([]byte(agentFixture()))
	c, e := parseCollector([]byte(collectorFixture()))
	return agentObservation{a, []scanTiming{{1, 1790670000000000000, 3000001, "failure"}, {2, 1790670000000000000, 3000001, "success"}}}, c, errors.Join(err, e)
}
func TestSamplingCancellationErrorsAndShortWrites(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	called := false
	if err := sampleSeries(ctx, io.Discard, 1, func() (agentObservation, collectorObservation, error) { called = true; return sampleFixture() }); err == nil || called {
		t.Fatal("cancelled sampling continued")
	}
	if err := sampleSeries(context.Background(), io.Discard, 1, func() (agentObservation, collectorObservation, error) {
		return agentObservation{}, collectorObservation{}, errObservation
	}); err == nil {
		t.Fatal("failed metrics became a record")
	}
	if err := sampleSeries(context.Background(), shortWriter{}, 1, sampleFixture); !errors.Is(err, io.ErrShortWrite) {
		t.Fatal(err)
	}
	for _, seconds := range []int{0, 1801} {
		if err := sampleSeries(context.Background(), io.Discard, seconds, sampleFixture); err == nil {
			t.Fatal("unbounded series")
		}
	}
}
func TestOneSecondSeriesHasInitialAndFinalNumericRecords(t *testing.T) {
	var out bytes.Buffer
	if err := sampleSeries(context.Background(), &out, 1, sampleFixture); err != nil {
		t.Fatal(err)
	}
	dec := json.NewDecoder(&out)
	var first, last observation
	if dec.Decode(&first) != nil || dec.Decode(&last) != nil || dec.Decode(new(any)) != io.EOF {
		t.Fatal("record inventory")
	}
	if first.SchemaVersion != 2 || last.SchemaVersion != 2 || len(first.AgentScans) != 2 || first.Index != 0 || first.ElapsedNanos != 0 || last.Index != 1 || last.ElapsedNanos < 1000000000 || first.Clock.Uncertainty > 5000000 || last.ObserverCPUUsec < first.ObserverCPUUsec || first.ObserverPeakRSSBytes <= 0 {
		t.Fatal("clock or observer accounting")
	}
	if last.Agent["scanDurationNanos"] != 3000001 || last.Collector.DurationNanos != 1000001 {
		t.Fatal("numeric projection")
	}
}
