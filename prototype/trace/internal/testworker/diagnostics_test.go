package testworker

import (
	"io"
	"strings"
	"testing"
)

func TestDiagnosticLimitAppliesToIOCopy(t *testing.T) {
	data := strings.Repeat("x", 100000)
	reader := struct{ io.Reader }{strings.NewReader(data)}
	var diagnostics Diagnostics
	n, err := io.Copy(&diagnostics, reader)
	if err != nil || n != int64(len(data)) || len(diagnostics.String()) != 16384 {
		t.Fatal("compiler diagnostics bypassed retained-byte bound")
	}
}
