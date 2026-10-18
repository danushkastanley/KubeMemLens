package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestObservationModesPreservePrivateConfigurationAndKernelGuards(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "private.json")
	data := []byte(`{"workerBootID":"00000000-0000-0000-0000-000000000000","durationSeconds":30}`)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"normal", "ceiling", "paused-reader"} {
		var output bytes.Buffer
		if err := runObservation(t.Context(), mode, path, &output); err == nil || output.Len() != 0 {
			t.Fatal("wrong kernel reached an observation transport", mode)
		}
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := runCeiling(t.Context(), path, &output); err == nil || output.Len() != 0 {
		t.Fatal("public ceiling configuration accepted")
	}
}

func TestUnknownObservationDoesNotLoadConfigurationOrProduceEvidence(t *testing.T) {
	for _, mode := range []string{"", "flood", "Normal", "normal,ceiling"} {
		var output bytes.Buffer
		if err := runObservation(t.Context(), mode, "/does-not-exist", &output); err == nil || output.Len() != 0 {
			t.Fatal("unknown observation mode accepted")
		}
	}
}
