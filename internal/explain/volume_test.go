package explain

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/model"
	"github.com/danushkastanley/kube-memlens/internal/volumecontext"
	"github.com/danushkastanley/kube-memlens/internal/volumehealth"
)

func volumeNumber(v uint64) *uint64 { return &v }

func volumeInputFixture(t *testing.T, sources ...volumehealth.Source) VolumeInput {
	t.Helper()
	now := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	scope := volumecontext.PodScope{Namespace: "private-tenant", PodName: "private-app", PodUID: "private-uid", NodeName: "private-node", NodeUID: "private-node-uid", CreatedAt: now.Add(-time.Hour)}
	binding := volumecontext.Binding{VolumeName: "private-volume", PVCName: "private-claim", PVCUID: "private-claim-uid", PVCCreatedAt: now.Add(-time.Hour), Driver: "private.csi.example", ClaimAvailability: volumehealth.Reported, Configuration: volumecontext.Configuration{Kind: volumecontext.PersistentClaim, MountCount: 1}}
	raw := volumecontext.RawUsage{Namespace: scope.Namespace, PodUID: scope.PodUID, NodeUID: scope.NodeUID, VolumeName: binding.VolumeName, PVCNamespace: scope.Namespace, PVCName: binding.PVCName,
		Filesystem: volumecontext.Filesystem{CapturedAt: now, CapacityBytes: volumeNumber(100), UsedBytes: volumeNumber(20), AvailableBytes: volumeNumber(70), Inodes: volumeNumber(100), InodesUsed: volumeNumber(10), InodesFree: volumeNumber(90)}}
	var health []volumecontext.HealthObservation
	for _, source := range sources {
		h := volumehealth.Observation{Identity: volumehealth.Identity{Namespace: scope.Namespace, PodName: scope.PodName, PodUID: scope.PodUID, NodeName: scope.NodeName, VolumeName: binding.VolumeName, PVCName: binding.PVCName, PVCUID: binding.PVCUID, Driver: binding.Driver}, Source: source, Scope: volumehealth.VolumeScope, Availability: volumehealth.Reported, ObservedAt: now}
		if source != volumehealth.ControllerSource {
			h.Conditions = []volumehealth.Condition{{Status: "Degraded", Reason: "private-reason", Message: "private-backend"}}
		}
		if source == volumehealth.BackendSource {
			h.Scope = volumehealth.BackendScope
			h.Conditions[0].Status = "StorageDegraded"
		}
		health = append(health, volumecontext.HealthObservation{Observation: h, NodeUID: scope.NodeUID})
	}
	report, err := volumecontext.Join(scope, []volumecontext.Binding{binding}, []volumecontext.RawUsage{raw}, health, volumecontext.SourceState(volumehealth.Reported, ""), now)
	if err != nil {
		t.Fatal(err)
	}
	pod := api.PodSnapshot{Namespace: scope.Namespace, PodName: scope.PodName, PodUID: scope.PodUID, NodeName: scope.NodeName, CapturedAt: now,
		Memory:     model.MemoryBreakdown{TotalBytes: 100, AnonBytes: 60, FileBytes: 40},
		Containers: []api.ContainerSnapshot{{ContainerName: "private-container", ContainerID: "private-runtime", CapturedAt: now, Memory: model.MemoryBreakdown{IOPressure: model.IOPressure{State: model.IOAvailable}}}}}
	return VolumeInput{Pod: pod, Volumes: report.Authorised(), Now: now}
}

func hasVolumeSignal(r VolumeResult, kind VolumeSignalKind) bool {
	for _, s := range r.Signals {
		if s.Kind == kind {
			return true
		}
	}
	return false
}

func TestVolumeExplanationSignals(t *testing.T) {
	for _, test := range []struct {
		name   string
		kind   VolumeSignalKind
		change func(*VolumeInput)
	}{
		{"filesystem", VolumeFilesystemPressure, func(i *VolumeInput) { i.Volumes.Volumes[0].Usage.Filesystem.UsedBytes = volumeNumber(95) }},
		{"inode", VolumeInodePressure, func(i *VolumeInput) { i.Volumes.Volumes[0].Usage.Filesystem.InodesFree = volumeNumber(0) }},
		{"cache context", VolumeCacheContext, func(*VolumeInput) {}},
		{"writeback", VolumeWriteback, func(i *VolumeInput) { i.Pod.Memory.WritebackBytes = 20 }},
		{"I/O", VolumeIOStall, func(i *VolumeInput) { i.Pod.Containers[0].Memory.IOPressure.Some.Avg10 = 5 }},
		{"shared memory", VolumeSharedMemory, func(i *VolumeInput) { i.Pod.Memory.ShmemBytes = 35 }},
		{"tmpfs configuration", VolumeTmpfsContext, func(i *VolumeInput) {
			v := &i.Volumes.Volumes[0]
			v.PVCName, v.Driver = "", ""
			v.Configuration = volumecontext.Configuration{Kind: volumecontext.EmptyDir, MemoryBacked: true, MountCount: 1}
		}},
		{"cache movement", VolumeCacheMovement, func(i *VolumeInput) {
			before := i.Pod
			before.CapturedAt = i.Now.Add(-10 * time.Second)
			before.Memory.FileBytes = 20
			i.Previous = &before
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			i := volumeInputFixture(t)
			test.change(&i)
			before, _ := json.Marshal(i)
			r := AnalyzeVolumes(i)
			if !hasVolumeSignal(r, test.kind) {
				t.Fatalf("missing %s: %+v", test.kind, r)
			}
			if r.MemorySeverity != AnalyzePodAt(i.Pod, i.Now).Severity {
				t.Fatal("storage changed memory severity")
			}
			after, _ := json.Marshal(i)
			if string(before) != string(after) {
				t.Fatal("analysis mutated source memory or volume data")
			}
			encoded, _ := json.Marshal(r)
			if strings.Contains(string(encoded), "private-") || strings.Contains(string(encoded), "private.csi") {
				t.Fatal("private identity/text entered explanation")
			}
			if !strings.Contains(strings.Join(r.Caveats, " "), "does not prove causality") {
				t.Fatal("causality caveat missing")
			}
		})
	}
}

func TestVolumeSourceConflictAndSkew(t *testing.T) {
	i := volumeInputFixture(t, volumehealth.PodSource, volumehealth.ControllerSource, volumehealth.BackendSource)
	i.Pod.Memory.DirtyBytes = 20
	r := AnalyzeVolumes(i)
	if !hasVolumeSignal(r, VolumeAdverseHealth) || !hasVolumeSignal(r, VolumeAlignedEvidence) || r.StorageSeverity != SeverityHigh {
		t.Fatalf("adverse conflict lost: %+v", r)
	}
	adverse := 0
	for _, s := range r.Signals {
		if s.Kind == VolumeAdverseHealth {
			adverse++
		}
	}
	if adverse != 2 || !strings.Contains(strings.Join(r.Caveats, " "), "Node/driver") {
		t.Fatal("health source separation lost")
	}
	i = volumeInputFixture(t)
	i.Pod.Memory.DirtyBytes = 20
	fs := i.Volumes.Volumes[0].Usage.Filesystem
	fs.UsedBytes = volumeNumber(95)
	fs.CapturedAt = i.Now.Add(-40 * time.Second)
	r = AnalyzeVolumes(i)
	if hasVolumeSignal(r, VolumeAlignedEvidence) || !strings.Contains(strings.Join(r.Caveats, " "), "30 seconds") {
		t.Fatal("skewed source correlated")
	}
	i.Volumes.Volumes[0].Usage.Freshness = volumehealth.Stale
	fs.CapturedAt = i.Now.Add(-60 * time.Second)
	r = AnalyzeVolumes(i)
	if r.StorageSeverity != SeverityInfo || hasVolumeSignal(r, VolumeAlignedEvidence) {
		t.Fatal("historical saturation became current risk")
	}
	for _, s := range r.Signals {
		if s.Kind == VolumeFilesystemPressure && !s.Historical {
			t.Fatal("stale filesystem age concealed")
		}
	}
}

func TestVolumeMissingEvidenceAndLifetime(t *testing.T) {
	i := volumeInputFixture(t)
	i.Volumes.Volumes[0].Usage = volumecontext.SourceState(volumehealth.Unreported, volumecontext.NoReport)
	i.Pod.Containers[0].Memory.IOPressure = model.IOPressure{State: model.IOUnreported}
	r := AnalyzeVolumes(i)
	if hasVolumeSignal(r, VolumeFilesystemPressure) || hasVolumeSignal(r, VolumeIOStall) || r.IO.Available != 0 || r.IO.Unreported != 1 {
		t.Fatal("missing evidence became measured zero or pressure")
	}
	if !strings.Contains(strings.Join(r.Caveats, " "), "no-report") {
		t.Fatal("missing source reason lost")
	}
	before := i.Pod
	before.PodUID = "replacement"
	before.CapturedAt = i.Now.Add(-time.Second)
	before.Memory.FileBytes = 2
	i.Previous = &before
	if hasVolumeSignal(AnalyzeVolumes(i), VolumeCacheMovement) {
		t.Fatal("replacement inherited cache movement")
	}
	before.PodUID = i.Pod.PodUID
	before.Containers = append([]api.ContainerSnapshot(nil), i.Pod.Containers...)
	before.Containers[0].ContainerID = "replacement"
	if hasVolumeSignal(AnalyzeVolumes(i), VolumeCacheMovement) {
		t.Fatal("container replacement inherited cache movement")
	}
	i.Pod.Freshness = api.EvidenceFreshnessStale
	i.Pod.Memory.DirtyBytes = 20
	if hasVolumeSignal(AnalyzeVolumes(i), VolumeWriteback) {
		t.Fatal("stale memory used for correlation")
	}
	i.Volumes.Volumes[0].Usage.Reason = "private-backend-identifier"
	encoded, _ := json.Marshal(AnalyzeVolumes(i))
	if strings.Contains(string(encoded), "private-backend") {
		t.Fatal("unvalidated source text disclosed")
	}
}

func TestIOCoverageUsesMaximaAndKeepsMissingAndResets(t *testing.T) {
	i := volumeInputFixture(t)
	c := i.Pod.Containers[0]
	c.Memory.IOPressure.Some.Avg10 = 60
	c.Memory.IOPressure.CounterReset = true
	i.Pod.Containers = []api.ContainerSnapshot{c, c, c}
	i.Pod.Containers[1].Memory.IOPressure.Some.Avg10 = 80
	i.Pod.Containers[2].Memory.IOPressure = model.IOPressure{State: model.IOUnavailable}
	r := AnalyzeVolumes(i)
	if r.IO.SomeMax10 != 80 || r.IO.Available != 2 || r.IO.Unavailable != 1 || r.IO.Resets != 2 {
		t.Fatalf("I/O coverage = %+v", r.IO)
	}
}
