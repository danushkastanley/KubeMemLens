package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrivateConfigurationAndClockGuardPrecedeNetwork(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	value := `{"workerBootID":"00000000-0000-0000-0000-000000000000","durationSeconds":30}`
	if err := os.WriteFile(path, []byte(value), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := load(path); err == nil {
		t.Fatal("public configuration accepted")
	}
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := load(path); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := run(context.Background(), path, &output); err == nil || output.Len() != 0 {
		t.Fatal("wrong kernel reached the transport")
	}
	link := filepath.Join(dir, "link.json")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := load(link); err == nil {
		t.Fatal("symbolic configuration accepted")
	}
	for _, bad := range []string{value + ` {}`, value + strings.Repeat(" ", 32769), strings.Replace(value, `"durationSeconds":30`, `"durationSeconds":31`, 1), strings.Replace(value, `"durationSeconds":30`, `"unknown":1,"durationSeconds":30`, 1)} {
		if err := os.WriteFile(path, []byte(bad), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := load(path); err == nil {
			t.Fatal("unbounded or ambiguous configuration accepted")
		}
	}
	cfg := privateConfig{Token: "private-token"}
	if strings.Contains(fmt.Sprintf("%+v", cfg), cfg.Token) {
		t.Fatal("configuration formatting disclosed token")
	}
}
