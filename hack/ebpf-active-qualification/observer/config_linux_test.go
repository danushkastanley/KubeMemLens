package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func configFixture(t *testing.T) []byte {
	t.Helper()
	value := map[string]any{"seconds": 1, "bootID": "12345678-1234-1234-1234-123456789abc", "server": "https://127.0.0.1:6443", "token": "private-token", "caPEM": "private-ca", "agentPID": 10, "agentStart": 100, "agentContainer": strings.Repeat("a", 64), "agentSHA256": strings.Repeat("b", 64), "collectorPID": 11, "collectorStart": 110, "collectorContainer": strings.Repeat("c", 64), "collectorSHA256": strings.Repeat("d", 64)}
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
func TestPrivateConfigBoundsAndAmbiguity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	raw := configFixture(t)
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(fmt.Sprintf("%+v", cfg), "private-token") {
		t.Fatal("formatted secret")
	}
	if data, err := json.Marshal(cfg); err == nil || strings.Contains(string(data), "private-token") {
		t.Fatal("encoded secret")
	}
	for _, data := range []string{string(raw) + "{}", strings.Replace(string(raw), `"seconds":1`, `"seconds":1801`, 1), strings.Replace(string(raw), `"seconds":1`, `"seconds":1,"seconds":1`, 1), strings.Replace(string(raw), `"seconds":1`, `"Seconds":1`, 1), strings.Replace(string(raw), `"seconds":1`, `"seconds":0`, 1), strings.Replace(string(raw), `"collectorPID":11`, `"collectorPID":10`, 1), strings.Repeat(" ", 32769)} {
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := loadConfig(path); err == nil {
			t.Fatal("invalid configuration accepted")
		}
	}
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadConfig(path); err == nil {
		t.Fatal("public file accepted")
	}
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	link := path + ".link"
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := loadConfig(link); err == nil {
		t.Fatal("symlink followed")
	}
	fifo := path + ".fifo"
	if err := syscall.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadConfig(fifo); err == nil {
		t.Fatal("FIFO accepted")
	}
}

func TestOptionalNetworkScopeRetainsLocalConfigAndRejectsInvalidScope(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	raw := string(configFixture(t))
	for _, scope := range []string{`"local"`, `"eks"`, `null`, `""`, `"automatic"`, `"eks","networkScope":"local"`} {
		data := strings.TrimSuffix(raw, "}") + `,"networkScope":` + scope + "}"
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		cfg, err := loadConfig(path)
		valid := scope == `"local"` || scope == `"eks"`
		if (err == nil) != valid {
			t.Fatalf("scope validity mismatch: %s", scope)
		}
		if valid && string(cfg.NetworkScope) != strings.Trim(scope, `"`) {
			t.Fatal("scope lost")
		}
	}
}
