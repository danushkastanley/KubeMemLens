package incident

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/client"
	"github.com/danushkastanley/kube-memlens/internal/model"
	"github.com/danushkastanley/kube-memlens/internal/volumecontext"
	"github.com/danushkastanley/kube-memlens/internal/volumehealth"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func volumeCaptureFixture(t *testing.T) (api.PodSnapshot, api.PodVolumeContext, *api.PodHistory, time.Time) {
	t.Helper()
	now := time.Now().UTC()
	used := uint64(42)
	scope := volumecontext.PodScope{Namespace: "private-team", PodName: "private-pod", PodUID: "private-pod-uid", NodeName: "private-node", NodeUID: "private-node-uid", CreatedAt: now.Add(-time.Hour)}
	binding := volumecontext.Binding{VolumeName: "private-volume", PVCName: "private-claim", PVCUID: "private-claim-uid", PVUID: "private-pv-uid", PVCCreatedAt: scope.CreatedAt, Driver: "private.csi.test", ClaimAvailability: volumehealth.Reported, Configuration: volumecontext.Configuration{Kind: volumecontext.PersistentClaim, MountCount: 1}}
	health := volumecontext.HealthObservation{NodeUID: scope.NodeUID, Observation: volumehealth.Observation{Identity: volumehealth.Identity{Namespace: scope.Namespace, PodName: scope.PodName, PodUID: scope.PodUID, NodeName: scope.NodeName, VolumeName: binding.VolumeName, PVCName: binding.PVCName, PVCUID: binding.PVCUID, Driver: binding.Driver}, Source: volumehealth.PodSource, Scope: volumehealth.VolumeScope, Availability: volumehealth.Reported, ObservedAt: now, Conditions: []volumehealth.Condition{{Status: "private-future-status", Reason: "private-reason", Message: "private-backend"}}}}
	report, err := volumecontext.Join(scope, []volumecontext.Binding{binding}, []volumecontext.RawUsage{{Namespace: scope.Namespace, PodUID: scope.PodUID, NodeUID: scope.NodeUID, VolumeName: binding.VolumeName, PVCNamespace: scope.Namespace, PVCName: binding.PVCName, Filesystem: volumecontext.Filesystem{CapturedAt: now, UsedBytes: &used}}}, []volumecontext.HealthObservation{health}, volumecontext.SourceState(volumehealth.Reported, ""), now)
	if err != nil {
		t.Fatal(err)
	}
	memory := model.MemoryBreakdown{Name: "private-memory", TotalBytes: 100, AnonBytes: 60, FileBytes: 40, IOPressure: model.IOPressure{State: model.IOAvailable, Some: model.PSIWindow{Avg10: 3, TotalMicros: 100}}}
	pod := api.PodSnapshot{Namespace: scope.Namespace, PodName: scope.PodName, PodUID: scope.PodUID, NodeName: scope.NodeName, CapturedAt: now, Memory: memory, Context: api.PodContext{OwnerKind: "Deployment", OwnerName: "private-owner", WorkloadKind: "Deployment", WorkloadName: "private-workload", RuntimeClassName: "private-runtime", LastTerminationReason: "private-termination", Labels: map[string]string{"private-label": "private-value"}}}
	pod.Containers = []api.ContainerSnapshot{{Namespace: pod.Namespace, PodName: pod.PodName, PodUID: pod.PodUID, NodeName: pod.NodeName, ContainerName: "private-container", ContainerID: "private-container-id", CgroupPath: "/private/cgroup", CapturedAt: now, Memory: memory, Context: api.ContainerContext{RuntimeClassName: "private-runtime", OwnerName: "private-owner", Labels: pod.Context.Labels}}}
	volumes := api.PodVolumeContext{TypeMeta: metav1.TypeMeta{APIVersion: api.MemoryAPIGroup + "/" + api.MemoryAPIVersion, Kind: "PodVolumeContext"}, ObjectMeta: metav1.ObjectMeta{Namespace: pod.Namespace, Name: pod.PodName, UID: types.UID(pod.PodUID)}, Context: report.Authorised()}
	history := &api.PodHistory{Namespace: pod.Namespace, PodName: pod.PodName, PodUID: pod.PodUID, NodeName: pod.NodeName, Points: []api.MemoryHistoryPoint{{CapturedAt: now.Add(-5 * time.Second), TotalBytes: 90}, {CapturedAt: now, TotalBytes: 100}}}
	return pod, volumes, history, now
}

func TestVolumeCaptureRedactionReplayAndLegacy(t *testing.T) {
	pod, volumes, history, now := volumeCaptureFixture(t)
	source, _ := json.Marshal(volumes)
	b, err := NewVolume(pod, volumes, history, "v-test", now, false)
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "private") || strings.Contains(string(body), "evidenceID") || strings.Contains(string(body), "filesystemID") {
		t.Fatalf("capture leaked identity: %s", body)
	}
	if !b.Volumes.Volumes[0].Health[0].Observation.Adverse || !b.Volumes.Volumes[0].Health[0].Observation.UnknownStatus || b.Pod.Containers[0].Memory.IOPressure.Some.Avg10 != 3 {
		t.Fatal("redaction erased evidence")
	}
	after, _ := json.Marshal(volumes)
	if !bytes.Equal(source, after) || pod.PodUID == "" {
		t.Fatal("live source mutated")
	}
	path := filepath.Join(t.TempDir(), "capture.json")
	if err := WriteVolume(nil, path, false, b); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Fatal("capture is not private")
	}
	doc, err := Read(path)
	if err != nil || doc.Volume == nil {
		t.Fatal("capture cannot be replayed", err)
	}
	if err := WriteVolume(nil, path, false, b); err == nil {
		t.Fatal("overwrite did not require confirmation")
	}
	for _, schema := range []int{1, 2} {
		legacy, err := LegacyVolume(b, schema)
		if err != nil {
			t.Fatal(err)
		}
		if err := ValidateSchema(legacy); err != nil {
			t.Fatal(err)
		}
		data, _ := json.Marshal(legacy)
		if strings.Contains(string(data), `"ioPressure"`) || strings.Contains(string(data), `"volumes"`) || !legacy.Partial {
			t.Fatal("legacy export retained incompatible enrichment")
		}
	}
}

func TestVolumeIncidentRejectsMalformedAndFalseRedaction(t *testing.T) {
	p, v, h, now := volumeCaptureFixture(t)
	b, err := NewVolume(p, v, h, "v-test", now, true)
	if err != nil {
		t.Fatal(err)
	}
	b.Redacted = true
	if ValidateVolume(b) == nil {
		t.Fatal("false redaction accepted")
	}
	b.Redacted = false
	data, _ := json.Marshal(b)
	for _, body := range [][]byte{
		bytes.Replace(data, []byte(`"schemaVersion":5`), []byte(`"schemaVersion":5,"SchemaVersion":5`), 1),
		bytes.Replace(data, []byte(`"redacted":false`), []byte(`"redacted":null`), 1),
		bytes.Replace(data, []byte(`"redacted":false`), []byte(`"Redacted":null`), 1),
		bytes.Replace(data, []byte(`"redacted":false,`), nil, 1),
		append(data, []byte(` {}`)...),
		bytes.Repeat([]byte("x"), MaxVolumeBytes+1),
	} {
		if _, err := decodeVolume(body); err == nil {
			t.Fatal("invalid volume JSON accepted")
		}
	}
	b.Caveats = append(b.Caveats, "private-backend")
	if ValidateVolume(b) == nil {
		t.Fatal("arbitrary caveat text accepted")
	}
}

type captureVolumeReader struct {
	pod       api.PodSnapshot
	volumes   api.PodVolumeContext
	history   []api.PodHistory
	volumeErr error
	calls     int
}

func (r *captureVolumeReader) Pod(context.Context, string, string) (api.PodSnapshot, error) {
	return r.pod, nil
}
func (r *captureVolumeReader) PodVolumes(context.Context, string, string, string) (api.PodVolumeContext, error) {
	r.calls++
	return r.volumes, r.volumeErr
}
func (r *captureVolumeReader) PodHistory(context.Context, string, string) ([]api.PodHistory, error) {
	return r.history, nil
}

func TestCollectVolumeReauthorisesEveryAttempt(t *testing.T) {
	p, v, h, _ := volumeCaptureFixture(t)
	r := &captureVolumeReader{pod: p, volumes: v, history: []api.PodHistory{*h}}
	opts := VolumeCaptureOptions{ExpectedUID: p.PodUID, IncludeHistory: true, ToolVersion: "v-test"}
	if _, err := CollectVolume(t.Context(), r, p.Namespace, p.PodName, opts); err != nil {
		t.Fatal(err)
	}
	r.volumeErr = &client.ReadError{Kind: client.ReadErrorForbidden}
	b, err := CollectVolume(t.Context(), r, p.Namespace, p.PodName, opts)
	if !client.IsForbidden(err) || r.calls != 2 || b.SchemaVersion != 0 {
		t.Fatal("cached authorisation or partial capture used")
	}
}

func TestVolumeIncidentRejectsInvalidMemoryPressure(t *testing.T) {
	for _, value := range []float64{-1, 101} {
		p, v, h, now := volumeCaptureFixture(t)
		p.Memory.PSISomeAvg10 = value
		if _, err := NewVolume(p, v, h, "v-test", now, true); err == nil {
			t.Fatal("invalid memory PSI accepted")
		}
		p.Memory.PSISomeAvg10 = 0
		h.Points[0].PSIFullAvg10 = value
		if _, err := NewVolume(p, v, h, "v-test", now, true); err == nil {
			t.Fatal("invalid history PSI accepted")
		}
	}
}
