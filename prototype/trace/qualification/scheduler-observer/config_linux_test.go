package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixture() configuration {
	hashes := map[string]string{}
	for _, name := range names {
		hashes[name] = strings.Repeat("a", 64)
	}
	return configuration{Anchor: anchorConfiguration{PID: 42, Start: 100}, Seconds: 1, BootID: "00000000-0000-0000-0000-000000000000", OnlineCPUs: "0-13", TracepointSHA256: hashes}
}

func TestPrivateConfigRequiresCompleteUnambiguousBounds(t *testing.T) {
	path := filepath.Join(t.TempDir(), "input.json")
	raw, _ := json.Marshal(fixture())
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := load(path); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{string(raw) + "{}",
		strings.Replace(string(raw), `"anchor":{"pid":42,"start":100}`, `"anchor":null`, 1),
		strings.Replace(string(raw), `"pid":42`, `"pid":1`, 1),
		strings.Replace(string(raw), `"start":100`, `"start":100,"start":101`, 1), strings.Replace(string(raw), `"seconds":1`, `"seconds":1801`, 1), strings.Replace(string(raw), `"seconds":1`, `"seconds":1,"seconds":1`, 1), strings.Replace(string(raw), `"seconds":1`, `"Seconds":1`, 1), strings.Replace(string(raw), `"seconds":1`, `"seconds":null`, 1), strings.Replace(string(raw), `"onlineCPUs":"0-13"`, `"onlineCPUs":"0-64"`, 1), strings.Replace(string(raw), `"sched_switch":`, `"foreign":`, 1)} {
		if err := os.WriteFile(path, []byte(bad), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := load(path); err == nil {
			t.Fatal("invalid config accepted")
		}
	}
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := load(path); err == nil {
		t.Fatal("public configuration accepted")
	}
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(path, path+".link"); err != nil {
		t.Fatal(err)
	}
	if _, err := load(path + ".link"); err == nil {
		t.Fatal("symlink followed")
	}
}

func TestCPUSetIsBoundedOrderedAndUnique(t *testing.T) {
	for _, bad := range []string{"", "0-64", "0,0", "2,1", "0-0", "0-1-2", "4096", "-1", "0,,1"} {
		if _, err := cpuList(bad); err == nil {
			t.Fatal("invalid CPUs", bad)
		}
	}
	cpus, err := cpuList("0-2,4,6-7")
	if err != nil || len(cpus) != 6 || cpus[3] != 4 || cpus[5] != 7 {
		t.Fatal(cpus, err)
	}
}

func TestCancelledOrWrongBootCannotStartCapture(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var output bytes.Buffer
	if err := run(ctx, configuration{}, &output); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if run(context.Background(), fixture(), &output) == nil || output.Len() != 0 {
		t.Fatal("wrong boot reached capture")
	}
}
