package cgroup

import (
	"io/fs"
	"strings"
	"syscall"
	"testing"
	"testing/fstest"
)

const memoryPressureSample = "some avg10=1.50 avg60=0.50 avg300=0.10 total=1234\nfull avg10=0.10 avg60=0.05 avg300=0.01 total=42\n"

// unreadableFS fails reads of one file the way a kernel with PSI compiled in
// but disabled at boot does: the file exists and reading it returns an error.
type unreadableFS struct {
	fs.FS
	name string
	err  error
}

func (f unreadableFS) Open(name string) (fs.File, error) {
	if name == f.name {
		return nil, &fs.PathError{Op: "read", Path: name, Err: f.err}
	}
	return f.FS.Open(name)
}

func TestUnusableMemoryPressurePreservesMemory(t *testing.T) {
	base := func(pressure string) fstest.MapFS {
		return fstest.MapFS{
			"memory.current":  {Data: []byte("4096\n")},
			"memory.stat":     {Data: []byte("anon 2048\nfile 1024\n")},
			"memory.pressure": {Data: []byte(pressure)},
		}
	}
	tests := map[string]fs.FS{
		"PSI disabled at boot": unreadableFS{FS: base(memoryPressureSample), name: "memory.pressure", err: syscall.EOPNOTSUPP},
		"permission denied":    unreadableFS{FS: base(memoryPressureSample), name: "memory.pressure", err: fs.ErrPermission},
		"unparseable":          base("invalid\n"),
		"one class only":       base("some avg10=0 avg60=0 avg300=0 total=0\n"),
		"not a number":         base(strings.Replace(memoryPressureSample, "avg10=1.50", "avg10=NaN", 1)),
		"infinite":             base(strings.Replace(memoryPressureSample, "avg60=0.50", "avg60=+Inf", 1)),
		"above 100 per cent":   base(strings.Replace(memoryPressureSample, "avg300=0.10", "avg300=100.01", 1)),
		"negative":             base(strings.Replace(memoryPressureSample, "avg10=0.10", "avg10=-0.10", 1)),
	}
	for name, fsys := range tests {
		t.Run(name, func(t *testing.T) {
			got, err := ParseFS("fixture", fsys)
			if err != nil {
				t.Fatalf("memory sample lost: %v", err)
			}
			if got.TotalBytes != 4096 || got.AnonBytes != 2048 {
				t.Fatalf("memory sample changed: %+v", got)
			}
			if got.PressureKnown || got.PSISomeAvg10 != 0 || got.PSIFullAvg10 != 0 || got.PSISomeTotalMicros != 0 {
				t.Fatalf("unusable pressure reported as known: %+v", got)
			}
		})
	}

	got, err := ParseFS("fixture", base(memoryPressureSample))
	if err != nil || !got.PressureKnown || got.PSISomeAvg10 != 1.5 || got.PSIFullTotalMicros != 42 {
		t.Fatalf("usable pressure not reported: %+v, %v", got, err)
	}
}

func TestParseMemoryPressureAcceptsOnlyFinitePercentages(t *testing.T) {
	for value, valid := range map[string]bool{
		"0": true, "100": true, "99.99": true,
		"NaN": false, "Inf": false, "-Inf": false, "-0.01": false, "100.01": false, "1e3": false,
	} {
		sample := "some avg10=" + value + " avg60=0 avg300=0 total=0\nfull avg10=0 avg60=0 avg300=0 total=0\n"
		if _, err := ParseMemoryPressure([]byte(sample)); (err == nil) != valid {
			t.Fatalf("avg10=%s: error = %v, want valid = %t", value, err, valid)
		}
	}
}
