package nextcommand

import (
	"strings"
	"testing"
)

// shellSpecial is every byte a POSIX shell, or a terminal, treats as other
// than a literal part of a word.
const shellSpecial = " \t\r\n;&|<>()$`\\\"'*?[]{}#~!^=%\x1b\x7f"

func FuzzAcceptedValuesAreInertInAShell(f *testing.F) {
	for _, seed := range []string{"api", "team-a", "node-1.example", "Deployment", "x;id", "a b", "$(id)", "a\x1b[31m", "é", ""} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, value string) {
		if !SafeNames(value) && !SafeKind(value) {
			return
		}
		if strings.ContainsAny(value, shellSpecial) {
			t.Fatalf("value %q is accepted for a copyable command but contains a shell or terminal character", value)
		}
		for _, r := range value {
			if r > 0x7e {
				t.Fatalf("value %q is accepted but is not printable ASCII", value)
			}
		}
	})
}

func TestKubernetesNamesAndKindsAreAccepted(t *testing.T) {
	if !SafeNames("team-a", "api-7d9f8b6c5-x2x4q", "ip-10-0-0-1.eu-west-1.compute.internal") {
		t.Fatal("ordinary namespace, Pod and Node names are refused")
	}
	for _, kind := range []string{"Deployment", "StatefulSet", "Rollout", "job"} {
		if !SafeKind(kind) {
			t.Fatalf("kind %q is refused", kind)
		}
	}
	if SafeNames("api", "") {
		t.Fatal("an empty value must be refused")
	}
}
