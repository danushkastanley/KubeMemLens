package cgroup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseMemoryCurrent(t *testing.T) {
	got, err := ParseMemoryCurrent([]byte("1048576\n"))
	if err != nil {
		t.Fatalf("ParseMemoryCurrent returned error: %v", err)
	}
	if got != 1048576 {
		t.Fatalf("ParseMemoryCurrent = %d, want 1048576", got)
	}
}

func TestParseMemoryCurrentInvalid(t *testing.T) {
	if _, err := ParseMemoryCurrent([]byte("not-a-number\n")); err == nil {
		t.Fatal("ParseMemoryCurrent returned nil error for invalid input")
	}
}

func TestParseKeyedFileKeepsKnownAndUnknownKeys(t *testing.T) {
	tests := map[string]struct {
		parse       func([]byte) (values, unknown map[string]uint64, err error)
		lines       []string
		wantValues  map[string]uint64
		wantUnknown map[string]uint64
	}{
		"memory.stat": {
			parse: func(data []byte) (map[string]uint64, map[string]uint64, error) {
				stat, err := ParseMemoryStat(data)
				return stat.Values, stat.Unknown, err
			},
			lines:       []string{"anon 100", "file 200", "active_file 150", "slab 20", "future_kernel_key 42"},
			wantValues:  map[string]uint64{"anon": 100, "file": 200},
			wantUnknown: map[string]uint64{"future_kernel_key": 42},
		},
		"memory.events": {
			parse: func(data []byte) (map[string]uint64, map[string]uint64, error) {
				events, err := ParseMemoryEvents(data)
				return events.Values, events.Unknown, err
			},
			lines:       []string{"low 0", "high 3", "max 4", "oom 1", "oom_kill 1", "future_event 9"},
			wantValues:  map[string]uint64{"oom": 1, "high": 3},
			wantUnknown: map[string]uint64{"future_event": 9},
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			values, unknown, err := test.parse([]byte(strings.Join(test.lines, "\n")))
			if err != nil {
				t.Fatalf("parse returned error: %v", err)
			}
			for key, want := range test.wantValues {
				if values[key] != want {
					t.Fatalf("%s = %d, want %d", key, values[key], want)
				}
			}
			for key, want := range test.wantUnknown {
				if unknown[key] != want {
					t.Fatalf("unknown key %s not preserved: %#v", key, unknown)
				}
			}
		})
	}
}

func TestParseMemoryStatInvalidValue(t *testing.T) {
	if _, err := ParseMemoryStat([]byte("anon nope\n")); err == nil {
		t.Fatal("ParseMemoryStat returned nil error for invalid value")
	}
}

func TestParseDirectoryMissingFile(t *testing.T) {
	_, err := ParseDirectory("missing", t.TempDir())
	if err == nil {
		t.Fatal("ParseDirectory returned nil error for missing files")
	}
	if !strings.Contains(err.Error(), "memory.current") {
		t.Fatalf("error = %q, want memory.current context", err.Error())
	}
}

func TestParseDirectoryAllowsMissingEvents(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "memory.current", "1024\n")
	writeFile(t, dir, "memory.stat", "anon 100\nfile 200\nslab 10\n")

	breakdown, err := ParseDirectory("sample", dir)
	if err != nil {
		t.Fatalf("ParseDirectory returned error: %v", err)
	}

	if breakdown.OOMEvents != 0 || breakdown.OOMKillEvents != 0 {
		t.Fatalf("unexpected OOM events: %#v", breakdown)
	}
}

func writeFile(t testHelper, dir, name, data string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}
