//go:build linux && (amd64 || arm64)

package workercontainment

import (
	"strings"
	"testing"
)

func TestCapabilityBudgetIncludesLatentPrivilege(t *testing.T) {
	valid := "CapEff:\t000000c000000000\nCapPrm:\t000000c000000000\nCapBnd:\t000000c000000000\nCapInh:\t0000000000000000\nCapAmb:\t0000000000000000\n"
	if checkCapabilities(valid) != nil {
		t.Fatal("exact capability budget rejected")
	}
	for _, name := range []string{"CapEff:", "CapPrm:", "CapBnd:", "CapInh:", "CapAmb:"} {
		lines := strings.Split(valid, "\n")
		for i, line := range lines {
			if strings.HasPrefix(line, name) {
				lines[i] = name + "\t000000c000200000"
			}
		}
		if checkCapabilities(strings.Join(lines, "\n")) == nil {
			t.Fatal("latent SYS_ADMIN accepted")
		}
	}
	for _, bad := range []string{strings.ReplaceAll(valid, "000000c000000000", "0000000000000000"), valid + "CapEff:\t000000c000000000\n", strings.Replace(valid, "CapAmb:", "Missing:", 1)} {
		if checkCapabilities(bad) == nil {
			t.Fatal("missing or ambiguous capability evidence accepted")
		}
	}
}
