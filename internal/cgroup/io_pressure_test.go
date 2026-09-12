package cgroup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/danushkastanley/kube-memlens/internal/model"
)

const ioSample = "some avg10=2.50 avg60=1.25 avg300=0.50 total=5000\nfull avg10=0.50 avg60=0.25 avg300=0.10 total=1000\n"

func TestIOPressureParsing(t *testing.T) {
	p, err := ParseIOPressure([]byte(ioSample))
	if err != nil || p.State != model.IOAvailable || p.Some.Avg10 != 2.5 || p.Full.TotalMicros != 1000 {
		t.Fatalf("I/O sample = %+v, %v", p, err)
	}
	zero := strings.NewReplacer("2.50", "0", "1.25", "0", "0.50", "0", "0.25", "0", "0.10", "0", "5000", "0", "1000", "0").Replace(ioSample)
	p, err = ParseIOPressure([]byte(zero))
	if err != nil || p.State != model.IOAvailable || p.Some != (model.PSIWindow{}) || p.Full != (model.PSIWindow{}) {
		t.Fatalf("measured zero lost: %+v, %v", p, err)
	}
	for _, input := range []string{
		"", "some avg10=0 avg60=0 avg300=0 total=0\n", ioSample + ioSample,
		strings.Replace(ioSample, "avg60=1.25", "avg10=1.25", 1),
		strings.Replace(ioSample, "avg60=1.25", "other=1.25", 1),
		strings.Replace(ioSample, "total=5000", "total=18446744073709551616", 1),
		strings.Repeat("x", maxIOPressureBytes+1),
	} {
		if _, err := ParseIOPressure([]byte(input)); err == nil {
			t.Fatal("invalid I/O sample accepted")
		}
	}
	for _, value := range []string{"NaN", "+Inf", "-Inf", "-1", "100.01"} {
		if _, err := ParseIOPressure([]byte(strings.Replace(ioSample, "2.50", value, 1))); err == nil {
			t.Fatalf("invalid percentage %s accepted", value)
		}
	}
}

func TestIOPressureFailurePreservesMemory(t *testing.T) {
	dir := t.TempDir()
	for name, value := range map[string]string{"memory.current": "4096", "memory.stat": "anon 2048\nfile 1024\n", "memory.events": "oom 0\n"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, test := range []struct {
		input string
		state model.IOState
	}{{"", model.IOUnreported}, {"invalid", model.IOInvalid}, {ioSample, model.IOAvailable}} {
		if test.input != "" {
			if err := os.WriteFile(filepath.Join(dir, "io.pressure"), []byte(test.input), 0600); err != nil {
				t.Fatal(err)
			}
		}
		m, err := ParseDirectory("fixture", dir)
		if err != nil || m.TotalBytes != 4096 || m.AnonBytes != 2048 || m.IOPressure.State != test.state {
			t.Fatalf("memory lost for %s: %+v, %v", test.state, m, err)
		}
	}
	if err := os.Remove(filepath.Join(dir, "io.pressure")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "io.pressure"), 0700); err != nil {
		t.Fatal(err)
	}
	if got := readIOPressure(dir); got.State != model.IOUnavailable {
		t.Fatalf("read failure = %+v", got)
	}
}

func FuzzIOPressure(f *testing.F) {
	f.Add([]byte(ioSample))
	f.Add([]byte("some avg10=NaN avg60=0 avg300=0 total=0\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		value, err := ParseIOPressure(data)
		if err == nil && value.Validate() != nil {
			t.Fatal("parser produced invalid observation")
		}
	})
}
