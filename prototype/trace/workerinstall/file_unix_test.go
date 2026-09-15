//go:build linux || darwin

package workerinstall

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPolicyFileSupportsContainedProjectionAndRejectsEscape(t *testing.T) {
	directory := t.TempDir()
	version := filepath.Join(directory, "version")
	if os.Mkdir(version, 0700) != nil {
		t.Fatal("fixture directory failed")
	}
	config := encoded(t, configuration(t))
	if os.WriteFile(filepath.Join(version, "policy.json"), config, 0444) != nil {
		t.Fatal("fixture write failed")
	}
	if os.Symlink("version", filepath.Join(directory, "..data")) != nil || os.Symlink("..data/policy.json", filepath.Join(directory, "policy.json")) != nil {
		t.Fatal("projection fixture failed")
	}
	policy, err := ReadFile(filepath.Join(directory, "policy.json"))
	if err != nil || policy.EngineDigest() == "" {
		t.Fatal("contained projected policy rejected")
	}
	outside := filepath.Join(t.TempDir(), "policy.json")
	if os.WriteFile(outside, config, 0444) != nil || os.Symlink(outside, filepath.Join(directory, "escape.json")) != nil {
		t.Fatal("escape fixture failed")
	}
	if _, err := ReadFile(filepath.Join(directory, "escape.json")); err == nil {
		t.Fatal("policy escaped its installation directory")
	}
	if os.WriteFile(filepath.Join(directory, "writable.json"), config, 0666) != nil || os.Chmod(filepath.Join(directory, "writable.json"), 0666) != nil {
		t.Fatal("permission fixture failed")
	}
	if _, err := ReadFile(filepath.Join(directory, "writable.json")); err == nil {
		t.Fatal("shared writable policy accepted")
	}
	if _, err := ReadFile("relative-policy.json"); err == nil {
		t.Fatal("relative policy accepted")
	}
}
