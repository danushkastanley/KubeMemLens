package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("closed reader") }

type shortWriter struct{}

func (shortWriter) Write(value []byte) (int, error) { return len(value) - 1, nil }

func TestEvidenceWriteFailuresAndCapsRemainExplicit(t *testing.T) {
	written := 0
	if writeRow(failingWriter{}, observation{}, &written) == nil {
		t.Fatal("reader failure hidden")
	}
	if writeRow(shortWriter{}, observation{}, &written) == nil {
		t.Fatal("partial write hidden")
	}
	row := observation{Binding: binding{Formats: map[string]string{"oversized": strings.Repeat("x", 8192)}}}
	var out bytes.Buffer
	if writeRow(&out, row, &written) == nil || out.Len() != 0 {
		t.Fatal("oversized row emitted")
	}
	written = 16 << 20
	if writeRow(&out, observation{}, &written) == nil || out.Len() != 0 {
		t.Fatal("stream bound widened")
	}
}

func TestSourceBindingUsesOnlyThreeOwnedFormatHashes(t *testing.T) {
	cfg := configurationFixture()
	hashes := map[string]string{}
	for _, role := range roles {
		hashes["kml_verifier_"+cfg.Owner+"/"+role+"_"+cfg.Owner] = strings.Repeat("d", 64)
	}
	bound, err := sourceBinding(cfg, hashes)
	if err != nil || len(bound.Formats) != 3 || bound.CgroupInode != cfg.Group.Inode {
		t.Fatal("source binding changed")
	}
	hashes["foreign"] = strings.Repeat("d", 64)
	if _, err := sourceBinding(cfg, hashes); err == nil {
		t.Fatal("foreign format entered evidence")
	}
}
