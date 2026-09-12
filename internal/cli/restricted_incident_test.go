package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/danushkastanley/kube-memlens/internal/incident"
)

func TestRestrictedCaptureOfflineReplayAndCompare(t *testing.T) {
	var status atomic.Int32
	config := restrictedConfig(t, &status)
	path := filepath.Join(t.TempDir(), "incident.json")
	run := func(args ...string) (string, error) {
		var output bytes.Buffer
		cmd := NewRootCommand(&output, &output)
		cmd.SetOut(&output)
		cmd.SetErr(&output)
		cmd.SetArgs(args)
		err := cmd.Execute()
		return output.String(), err
	}
	if out, err := run("--mode", "restricted", "--kubeconfig", config, "capture", "-n", "team-a", "-o", path); err != nil {
		t.Fatalf("capture: %v %s", err, out)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"pod-uid", "private-label", "private-value", "fixture-reader", "cgroup", "kubeconfig"} {
		if bytes.Contains(data, []byte(secret)) {
			t.Fatalf("capture leaked %s", secret)
		}
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("capture permissions: %v %v", info, err)
	}
	document, err := incident.Read(path)
	if err != nil || document.Restricted == nil {
		t.Fatalf("schema3 read: %#v %v", document, err)
	}
	if err := os.Remove(config); err != nil {
		t.Fatal(err)
	}
	first, err := run("--kubeconfig", "/missing/offline-config", "replay", path)
	if err != nil {
		t.Fatal(err)
	}
	second, err := run("replay", path)
	if err != nil || first != second {
		t.Fatalf("offline replay is not deterministic: %v", err)
	}
	for _, want := range []string{"Working set: 32Mi", "Coverage: 1/2", "partial", "deep evidence"} {
		if !strings.Contains(first, want) {
			t.Fatalf("replay lacks %q: %s", want, first)
		}
	}
	comparison, err := run("compare", "--before", path, "--after", path, "--pod", "team-a/app")
	if err != nil || !strings.Contains(comparison, "continuity is unconfirmed") || !strings.Contains(comparison, "delta: +0") {
		t.Fatalf("comparison: %v %s", err, comparison)
	}
}
