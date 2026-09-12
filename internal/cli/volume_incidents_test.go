package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/incident"
	"github.com/danushkastanley/kube-memlens/internal/model"
	"github.com/danushkastanley/kube-memlens/internal/volumecontext"
	"github.com/danushkastanley/kube-memlens/internal/volumehealth"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func cliVolumeIncident(t *testing.T, path string, at time.Time) {
	t.Helper()
	pod := api.PodSnapshot{Namespace: "private-team", PodName: "private-pod", PodUID: "private-uid", CapturedAt: at, Memory: model.MemoryBreakdown{TotalBytes: 100}, Containers: []api.ContainerSnapshot{{Namespace: "private-team", PodName: "private-pod", PodUID: "private-uid", ContainerName: "private-container", ContainerID: "private-runtime", CapturedAt: at, Memory: model.MemoryBreakdown{TotalBytes: 100}}}}
	value := api.PodVolumeContext{ObjectMeta: metav1.ObjectMeta{Namespace: pod.Namespace, Name: pod.PodName, UID: "private-uid"}, Context: volumecontext.View{SchemaVersion: 1, Namespace: pod.Namespace, PodName: pod.PodName, Volumes: []volumecontext.NamedVolume{{VolumeName: "private-volume", Configuration: volumecontext.Configuration{Kind: volumecontext.EmptyDir, MemoryBacked: true, MountCount: 1}, Usage: volumecontext.SourceState(volumehealth.Unreported, volumecontext.NoReport)}}}}
	b, err := incident.NewVolume(pod, value, nil, "v-test", at, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := incident.WriteVolume(nil, path, false, b); err != nil {
		t.Fatal(err)
	}
}

func TestVolumeCLIReplayCompareAndExplicitLegacyExport(t *testing.T) {
	dir := t.TempDir()
	before, after := filepath.Join(dir, "before.json"), filepath.Join(dir, "after.json")
	now := time.Now().UTC()
	cliVolumeIncident(t, before, now)
	cliVolumeIncident(t, after, now.Add(time.Second))
	run := func(args ...string) (string, error) {
		var out bytes.Buffer
		cmd := NewRootCommand(&out, &out)
		cmd.SetArgs(args)
		err := cmd.Execute()
		return out.String(), err
	}
	text, err := run("replay", before)
	if err != nil || !strings.Contains(text, "Volume incident captured") || strings.Contains(text, "private-") {
		t.Fatal("redacted replay failed", err, text)
	}
	text, err = run("compare", "--before", before, "--after", after, "--volumes")
	if err != nil || !strings.Contains(text, "aliases are not identities") || strings.Contains(text, "Used bytes change:") {
		t.Fatal("unlinked aliases produced deltas", err, text)
	}
	for _, selector := range [][]string{{"--node", "node-a"}, {"--pod", "other/pod"}} {
		args := append([]string{"replay", before, "--export-schema", "1", "--output", filepath.Join(dir, "mismatch.json")}, selector...)
		if _, err := run(args...); err == nil {
			t.Fatal("legacy export accepted a mismatched target")
		}
	}
	legacy := filepath.Join(dir, "legacy.json")
	if _, err := run("replay", before, "--export-schema", "1", "--output", legacy); err != nil {
		t.Fatal(err)
	}
	doc, err := incident.Read(legacy)
	if err != nil || doc.Deep == nil || doc.Deep.SchemaVersion != 1 || !doc.Deep.Partial {
		t.Fatal("legacy export incompatible", err)
	}
	info, _ := os.Stat(legacy)
	if info.Mode().Perm() != 0600 {
		t.Fatal("legacy export not private")
	}
	if _, err := run("replay", before, "--export-schema", "1", "--output", legacy); err == nil {
		t.Fatal("legacy overwrite lacked confirmation")
	}
	if _, err := run("compare", "--before", before, "--after", legacy, "--volumes"); err == nil {
		t.Fatal("incompatible incident domains compared")
	}
	for _, args := range [][]string{{"capture", "--volumes"}, {"capture", "--schema-version", "5", "--pod", "app"}, {"capture", "--volumes", "--node", "node"}, {"capture", "--volumes", "--pod", "app", "-n", "team", "--schema-version", "3"}} {
		if _, err := run(args...); err == nil {
			t.Fatal("invalid volume capture scope accepted")
		}
	}
}
