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

func TestOptionalScopeRejectsAmbiguousProviderSelection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	base := `{"workerBootID":"00000000-0000-0000-0000-000000000000","durationSeconds":30`
	for _, field := range []string{`"networkScope":"local"`, `"networkScope":"eks"`, `"networkScope":null`, `"networkScope":""`, `"networkScope":"automatic"`, `"networkScope":"local","networkScope":"eks"`, `"NetworkScope":"eks"`} {
		if err := os.WriteFile(path, []byte(base+","+field+"}"), 0600); err != nil {
			t.Fatal(err)
		}
		cfg, err := load(path)
		valid := field == `"networkScope":"local"` || field == `"networkScope":"eks"`
		if (err == nil) != valid {
			t.Fatalf("scope validity mismatch: %s", field)
		}
		if valid && cfg.NetworkScope == "" {
			t.Fatal("scope lost")
		}
	}
}
