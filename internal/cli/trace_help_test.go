package cli

import (
	"bytes"
	"strings"
	"testing"
)

// The trace commands ship in every release of the CLI, but the server they
// talk to does not. Help must say so before someone tries them.
func TestTraceHelpSaysTheExtensionIsBuiltSeparately(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"trace", "--help"}} {
		var out bytes.Buffer
		root := NewRootCommand(&out, &out)
		root.SetArgs(args)
		if err := root.Execute(); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), "separately built") {
			t.Fatalf("%v does not say the trace extension is separately built:\n%s", args, out.String())
		}
	}
	var out bytes.Buffer
	root := NewRootCommand(&out, &out)
	root.SetArgs([]string{"trace", "--help"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "not part of the released chart or image") {
		t.Fatalf("trace help does not say where the extension is missing from:\n%s", out.String())
	}
}
