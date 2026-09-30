//go:build linux || darwin

package workerinstall

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPinnedPolicyRejectsValidReplacement(t *testing.T) {
	path := filepath.Join(t.TempDir(), "policy.json")
	original := encoded(t, configuration(t))
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	digest := sum(original)
	if _, err := ReadPinnedFile(path, digest); err != nil {
		t.Fatal("exact installation policy rejected", err)
	}
	replacement := encoded(t, configuration(t))
	if sum(replacement) == digest {
		t.Fatal("replacement fixture did not change")
	}
	if err := os.WriteFile(path, replacement, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadFile(path); err != nil {
		t.Fatal("replacement must be independently valid", err)
	}
	if _, err := ReadPinnedFile(path, digest); err != ErrInstallation {
		t.Fatal("replacement bypassed installation digest")
	}
}

func TestPinnedPolicyRequiresCanonicalDigest(t *testing.T) {
	path := filepath.Join(t.TempDir(), "policy.json")
	data := encoded(t, configuration(t))
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	for _, digest := range []string{"", "sha256:" + sum(data), strings.ToUpper(sum(data)), strings.Repeat("0", 64), sum(data) + "\n"} {
		if _, err := ReadPinnedFile(path, digest); err != ErrInstallation {
			t.Fatal("invalid or mismatched digest accepted")
		}
	}
}
