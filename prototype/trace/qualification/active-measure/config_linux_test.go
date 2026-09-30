//go:build linux

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestActiveConfigurationHasIndependentBounds(t *testing.T) {
	for _, seconds := range []int{1, 900, 1050, 1800} {
		encoded, _ := json.Marshal(configuration{Seconds: seconds, Groups: []groupSpec{{"selected", "/sys/fs/cgroup/fixture", 1}}})
		if _, err := loadConfiguration(strings.NewReader(string(encoded))); err != nil {
			t.Fatal(err)
		}
	}
	for _, input := range []string{`{"seconds":0}`, `{"seconds":1801}`, `{"seconds":1050,"unknown":1}`, `{"seconds":2} {}`, `{"seconds":2,"groups":[{"role":"node","path":"/etc","inode":1}]}`} {
		if _, err := loadConfiguration(strings.NewReader(input)); err == nil {
			t.Fatalf("accepted %s", input)
		}
	}
	cfg := configuration{Seconds: 1, Groups: []groupSpec{{"node", "/sys/fs/cgroup/fixture", 1}, {"node", "/sys/fs/cgroup/other", 2}}}
	encoded, _ := json.Marshal(cfg)
	if _, err := loadConfiguration(strings.NewReader(string(encoded))); err == nil {
		t.Fatal("duplicate role")
	}
	cfg.Groups = make([]groupSpec, 21)
	encoded, _ = json.Marshal(cfg)
	if _, err := loadConfiguration(strings.NewReader(string(encoded))); err == nil {
		t.Fatal("group bound")
	}
}
func TestBindingsAreExplicitAndCannotEscapeCgroups(t *testing.T) {
	for _, role := range []string{"node", "api", "selected", "agent", "collector", "probe", "nonselected-0", "nonselected-9"} {
		if !validGroup(groupSpec{role, "/sys/fs/cgroup/fixture", 1}) {
			t.Fatal(role)
		}
	}
	for _, s := range []groupSpec{{"other", "/sys/fs/cgroup/a", 1}, {"nonselected-10", "/sys/fs/cgroup/a", 1}, {"node", "/sys/fs/cgroup/../a", 1}, {"node", "/sys/fs/cgroup/a", 0}, {"node", "/sys/fs/cgroup/", 1}} {
		if validGroup(s) {
			t.Fatalf("accepted %+v", s)
		}
	}
}
func TestCancelledObserverStopsBeforeARecord(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"seconds":1050,"groups":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var output bytes.Buffer
	if err := run(ctx, path, &output); err == nil {
		t.Fatal("cancelled observation succeeded")
	}
	if output.Len() != 0 {
		t.Fatal("cancelled observation wrote a record")
	}
}

func TestConfigurationCannotHideTrailingInputBeyondTheByteBound(t *testing.T) {
	for _, input := range []string{`{"seconds":1}` + strings.Repeat(" ", 16385), `{"seconds":1}` + strings.Repeat(" ", 16385) + `{"bad":true}`} {
		if _, err := loadConfiguration(strings.NewReader(input)); err == nil {
			t.Fatal("oversized input accepted")
		}
	}
}

func TestConfigurationRejectsAliasedGroupBindings(t *testing.T) {
	first := groupSpec{"node", "/sys/fs/cgroup/first", 1}
	for _, second := range []groupSpec{
		{"api", first.Path, 2},
		{"api", "/sys/fs/cgroup/second", first.Inode},
	} {
		encoded, err := json.Marshal(configuration{Seconds: 1, Groups: []groupSpec{first, second}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := loadConfiguration(bytes.NewReader(encoded)); err == nil {
			t.Fatal("aliased cgroup binding accepted")
		}
	}
}
