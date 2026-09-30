//go:build linux

package main

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

type partialWriter struct{}

func (partialWriter) Write(p []byte) (int, error) { return len(p) - 1, nil }

func TestOutputBoundsRejectBeforeDisclosure(t *testing.T) {
	var buf bytes.Buffer
	writer := recordWriter{output: &buf}
	r := record{Groups: map[string]groupSample{"node": {Memory: map[string]uint64{strings.Repeat("x", maxRecordBytes): 1}}}}
	if writer.write(r) == nil || buf.Len() != 0 {
		t.Fatal("oversized record was written")
	}
	writer.written = maxOutputBytes
	if writer.write(record{}) == nil || buf.Len() != 0 {
		t.Fatal("total output bound ignored")
	}
}
func TestPartialWriteCannotReportSuccess(t *testing.T) {
	writer := recordWriter{output: partialWriter{}}
	if !errors.Is(writer.write(record{}), io.ErrShortWrite) {
		t.Fatal("partial output appeared successful")
	}
}
func TestRecordsAreNDJSON(t *testing.T) {
	var buf bytes.Buffer
	writer := recordWriter{output: &buf}
	for i := 0; i < 2; i++ {
		if err := writer.write(record{SchemaVersion: 2, Index: i}); err != nil {
			t.Fatal(err)
		}
	}
	if strings.Count(buf.String(), "\n") != 2 || writer.written != buf.Len() {
		t.Fatal("invalid record accounting")
	}
}
