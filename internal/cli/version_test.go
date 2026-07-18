package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/danushkastanley/kube-memlens/internal/buildinfo"
)

func TestVersionCommandTextAndJSON(t *testing.T) {
	previousVersion, previousCommit, previousDate := buildinfo.Version, buildinfo.Commit, buildinfo.BuildDate
	t.Cleanup(func() {
		buildinfo.Version, buildinfo.Commit, buildinfo.BuildDate = previousVersion, previousCommit, previousDate
	})
	buildinfo.Version, buildinfo.Commit, buildinfo.BuildDate = "v0.5.0", "abc123", "2026-07-18T00:00:00Z"

	textOutput := &bytes.Buffer{}
	textCommand := newVersionCommand()
	textCommand.SetOut(textOutput)
	if err := textCommand.Execute(); err != nil {
		t.Fatalf("text version: %v", err)
	}
	if !strings.Contains(textOutput.String(), "version=v0.5.0 commit=abc123") {
		t.Fatalf("unexpected text output: %s", textOutput.String())
	}

	jsonOutput := &bytes.Buffer{}
	jsonCommand := newVersionCommand()
	jsonCommand.SetOut(jsonOutput)
	jsonCommand.SetArgs([]string{"--output=json"})
	if err := jsonCommand.Execute(); err != nil {
		t.Fatalf("JSON version: %v", err)
	}
	var info buildinfo.Info
	if err := json.Unmarshal(jsonOutput.Bytes(), &info); err != nil {
		t.Fatalf("decode JSON: %v", err)
	}
	if info.Version != "v0.5.0" || info.Commit != "abc123" {
		t.Fatalf("unexpected JSON info: %#v", info)
	}
}
