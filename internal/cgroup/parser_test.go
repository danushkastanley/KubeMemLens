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

func TestParseMemoryStat(t *testing.T) {
	stat, err := ParseMemoryStat([]byte(strings.Join([]string{
		"anon 100",
		"file 200",
		"active_file 150",
		"slab 20",
		"future_kernel_key 42",
	}, "\n")))
	if err != nil {
		t.Fatalf("ParseMemoryStat returned error: %v", err)
	}

	if stat.Values["anon"] != 100 {
		t.Fatalf("anon = %d, want 100", stat.Values["anon"])
	}
	if stat.Values["file"] != 200 {
		t.Fatalf("file = %d, want 200", stat.Values["file"])
	}
	if stat.Unknown["future_kernel_key"] != 42 {
		t.Fatalf("unknown key not preserved: %#v", stat.Unknown)
	}
}

func TestParseMemoryStatInvalidValue(t *testing.T) {
	if _, err := ParseMemoryStat([]byte("anon nope\n")); err == nil {
		t.Fatal("ParseMemoryStat returned nil error for invalid value")
	}
}

func TestParseMemoryEvents(t *testing.T) {
	events, err := ParseMemoryEvents([]byte(strings.Join([]string{
		"low 0",
		"high 3",
		"max 4",
		"oom 1",
		"oom_kill 1",
		"future_event 9",
	}, "\n")))
	if err != nil {
		t.Fatalf("ParseMemoryEvents returned error: %v", err)
	}

	if events.Values["oom"] != 1 {
		t.Fatalf("oom = %d, want 1", events.Values["oom"])
	}
	if events.Values["high"] != 3 {
		t.Fatalf("high = %d, want 3", events.Values["high"])
	}
	if events.Unknown["future_event"] != 9 {
		t.Fatalf("unknown event key not preserved: %#v", events.Unknown)
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

func writeFile(t *testing.T, dir, name, data string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}
