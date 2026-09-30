package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/danushkastanley/kube-memlens/internal/client"
	"github.com/danushkastanley/kube-memlens/internal/incidentsession"
)

func TestIncidentTraceCLIReferenceExportAndOfflineVerification(t *testing.T) {
	options := sessionCommandFixture(t)
	dir := t.TempDir()
	reportPath := filepath.Join(dir, "trace.json")
	data, err := os.ReadFile("../tracereport/testdata/schema2-v2-file-loss.json")
	if err != nil || os.WriteFile(reportPath, data, 0o600) != nil {
		t.Fatal("trace fixture unavailable", err)
	}
	output, err := runSessionCommand(t, options, "", "start", "-o", "json")
	var started incidentsession.Summary
	if err != nil || json.Unmarshal([]byte(output), &started) != nil {
		t.Fatal("start failed", err)
	}
	if _, err := runSessionCommand(t, options, "", "trace-reference", started.ID, "--report", reportPath); err == nil || !strings.Contains(err.Error(), "confirm-reference") {
		t.Fatal("missing consent accepted", err)
	}
	output, err = runSessionCommand(t, options, "", "trace-reference", started.ID, "--report", reportPath, "--confirm-reference")
	if err != nil || !strings.Contains(output, "trace-referenced") {
		t.Fatal("reference failed", err)
	}
	output, err = runSessionCommand(t, options, "", "show", started.ID)
	for _, want := range []string{"trace-1", "operator-supplied", "loss reported", "coverage incomplete", "unverified"} {
		if err != nil || !strings.Contains(output, want) {
			t.Fatal("timeline lost trace provenance or uncertainty", want, err)
		}
	}
	exportPath := filepath.Join(dir, "incident.json")
	if _, err := runSessionCommand(t, options, "", "export", started.ID, "--include-sensitive", "-o", exportPath); err != nil {
		t.Fatal(err)
	}
	offline := func() client.Options {
		t.Fatal("offline verification contacted cluster configuration")
		return client.Options{}
	}
	output, err = runSessionCommand(t, offline, "", "verify-trace", exportPath, "trace-1", "--report", reportPath)
	if err != nil || !strings.Contains(output, "Report bytes match") || !strings.Contains(output, "authenticity remains unverified") {
		t.Fatal("offline verification failed", err)
	}
	if err := os.WriteFile(reportPath, append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := runSessionCommand(t, offline, "", "verify-trace", exportPath, "trace-1", "--report", reportPath); err == nil {
		t.Fatal("changed report matched reference")
	}
}

func TestIncidentTraceConsentAndFileValidationPrecedeConnection(t *testing.T) {
	unreachable := func() client.Options {
		t.Fatal("invalid or unconfirmed report reached configuration")
		return client.Options{}
	}
	if _, err := runSessionCommand(t, unreachable, "", "trace-reference", strings.Repeat("a", 32), "--report", "/missing/private-file"); err == nil || !strings.Contains(err.Error(), "confirm-reference") {
		t.Fatal("consent was not checked first", err)
	}
	dir := t.TempDir()
	bad := filepath.Join(dir, "invalid.json")
	if err := os.WriteFile(bad, []byte(`{"secret":"private-value"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{bad, dir, "-"} {
		_, err := runSessionCommand(t, unreachable, "", "trace-reference", strings.Repeat("a", 32), "--report", path, "--confirm-reference")
		if err == nil || strings.Contains(err.Error(), "private-value") {
			t.Fatal("invalid local file sent or disclosed")
		}
	}
}
