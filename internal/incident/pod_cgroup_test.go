package incident

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/model"
)

func withSchema8Evidence(pod api.PodSnapshot) api.PodSnapshot {
	cgroup := model.PodCgroupMemory{MaxBytes: 256 << 20, MaxKnown: true}
	pod.PodCgroup = cgroup
	pod.Memory.Zswap = model.Zswap{Known: true, PoolBytes: 4, SwappedBytes: 12}
	pod.Containers = slices.Clone(pod.Containers)
	for index := range pod.Containers {
		pod.Containers[index].PodCgroup = cgroup
		pod.Containers[index].Memory.OOMGroupKill = model.OOMGroupKill{Known: true, Events: 1}
	}
	return pod
}

// Saved incident files of schemas 1 and 2 are read by released strict
// readers, so they never carry schema 8 evidence and say what was left out.
func TestDeepIncidentFileNeverKeepsSchema8Evidence(t *testing.T) {
	now := time.Now().UTC()
	pod := withSchema8Evidence(api.PodSnapshot{Namespace: "team", PodName: "app", CapturedAt: now, Containers: []api.ContainerSnapshot{{Namespace: "team", PodName: "app", ContainerName: "app", CapturedAt: now}}})
	path := filepath.Join(t.TempDir(), "capture.json")
	if err := Write(io.Discard, path, false, api.IncidentBundle{SchemaVersion: api.CurrentIncidentSchemaVersion, CapturedAt: now, ToolVersion: "v-test", Pods: []api.PodSnapshot{pod}}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if body := string(data); strings.Contains(body, `"podCgroup"`) || strings.Contains(body, `"zswap"`) || strings.Contains(body, `"oomGroupKill"`) {
		t.Fatalf("incident file keeps schema 8 evidence: %s", body)
	}
	document, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if !document.Deep.Partial || !slices.Contains(document.Deep.Caveats, api.PodCgroupIncidentCaveat) {
		t.Fatalf("omission not recorded: %#v", document.Deep.Caveats)
	}
}

// Volume schema 5 readers accept only their fixed caveats, so a volume
// capture leaves schema 8 evidence out and a file that carries it is refused.
func TestVolumeIncidentNeverKeepsSchema8Evidence(t *testing.T) {
	pod, volumes, history, now := volumeCaptureFixture(t)
	bundle, err := NewVolume(withSchema8Evidence(pod), volumes, history, "v-test", now, true)
	if err != nil {
		t.Fatal(err)
	}
	if api.PodHasSchema8Evidence(bundle.Pod) {
		t.Fatal("volume capture keeps schema 8 evidence")
	}
	if bundle.Pod.Containers[0].Memory.IOPressure.State != model.IOAvailable {
		t.Fatal("volume capture lost the I/O pressure it owns")
	}
	bundle.Pod = withSchema8Evidence(bundle.Pod)
	if ValidateVolume(bundle) == nil {
		encoded, _ := json.Marshal(bundle.Pod)
		t.Fatalf("volume validation accepted schema 8 evidence: %s", encoded)
	}
}
