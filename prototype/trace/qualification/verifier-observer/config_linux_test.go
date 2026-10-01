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

	"golang.org/x/sys/unix"
)

func configurationFixture() configuration {
	return configuration{Owner: strings.Repeat("a", 32), Seconds: 2, BootID: "11111111-1111-1111-1111-111111111111",
		OnlineCPUs: "0-13", BTFSHA256: strings.Repeat("b", 64), Anchor: anchorConfiguration{PID: 42, Start: 100, SHA256: strings.Repeat("c", 64)},
		Group: groupConfiguration{Path: "/sys/fs/cgroup/owned", Inode: 10}}
}

func TestPrivateConfigurationHasExactBoundedBindings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private.json")
	raw, _ := json.Marshal(configurationFixture())
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := load(path); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{string(raw) + "{}", "null", string(raw) + "\x00",
		strings.Replace(string(raw), `"seconds":2`, `"seconds":2,"seconds":2`, 1),
		strings.Replace(string(raw), `"seconds":2`, `"seconds":null`, 1),
		strings.Replace(string(raw), `"seconds":2`, `"seconds":1801`, 1),
		strings.Replace(string(raw), `"seconds":2`, `"Seconds":2`, 1),
		strings.Replace(string(raw), `"pid":42`, `"pid":1`, 1),
		strings.Replace(string(raw), `"start":100`, `"start":100,"start":101`, 1),
		strings.Replace(string(raw), `"inode":10`, `"inode":null`, 1),
		strings.Replace(string(raw), `"/sys/fs/cgroup/owned"`, `"/sys/fs/cgroup"`, 1),
		strings.Replace(string(raw), `"/sys/fs/cgroup/owned"`, `"/sys/fs/cgroup/../foreign"`, 1),
		strings.Replace(string(raw), `"0-13"`, `"0-64"`, 1),
	} {
		if err := os.WriteFile(path, []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := load(path); err == nil {
			t.Fatal("invalid observer configuration accepted")
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
		t.Fatal("configuration symlink followed")
	}
	if err := unix.Mkfifo(path+".fifo", 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := load(path + ".fifo"); err == nil {
		t.Fatal("blocking input accepted")
	}
}

func TestInvalidOrCancelledCaptureCannotRegisterProbes(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var out bytes.Buffer
	if err := run(ctx, configuration{}, &out); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled context reached setup")
	}
	if err := run(context.Background(), configurationFixture(), &out); err == nil {
		t.Fatal("wrong kernel binding accepted")
	}
	if out.Len() != 0 {
		t.Fatal("invalid observation produced evidence")
	}
}

func TestCPURosterIsOrderedUniqueAndBounded(t *testing.T) {
	for _, value := range []string{"", "0-64", "0,0", "1,0", "0-0", "-1", "4096", "0,,1"} {
		if _, err := cpuList(value); err == nil {
			t.Fatal("invalid CPU roster accepted")
		}
	}
	if values, err := cpuList("0-2,4,6-7"); err != nil || len(values) != 6 || values[3] != 4 {
		t.Fatal("valid CPU roster rejected")
	}
}
