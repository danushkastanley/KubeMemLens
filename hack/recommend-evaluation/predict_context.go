package main

import (
	"fmt"
	"github.com/danushkastanley/kube-memlens/internal/changemarkers"
	"github.com/danushkastanley/kube-memlens/internal/explain"
	"github.com/danushkastanley/kube-memlens/internal/recommend"
	"github.com/danushkastanley/kube-memlens/internal/replicabaseline"
	"github.com/danushkastanley/kube-memlens/internal/volumecontext"
	"github.com/danushkastanley/kube-memlens/internal/volumehealth"
	"strings"
	"time"
)

func predictVolume(c Case) (Prediction, error) {
	in := c.Volume
	scope := volumecontext.PodScope{Namespace: "fixture", PodName: "fixture", PodUID: "fixture-pod", NodeName: "fixture-node", NodeUID: "fixture-node-uid", CreatedAt: fixtureTime.Add(-time.Hour)}
	binding := volumecontext.Binding{VolumeName: "fixture-volume", PVCName: "fixture-claim", PVCUID: "fixture-claim-uid", PVCCreatedAt: fixtureTime.Add(-time.Hour), Driver: "fixture.example", ClaimAvailability: volumehealth.Reported,
		Configuration: volumecontext.Configuration{Kind: volumecontext.PersistentClaim, MountCount: 1}}
	claimNamespace := scope.Namespace
	if in.MemoryBacked {
		binding.PVCName, binding.PVCUID, binding.Driver = "", "", ""
		claimNamespace = ""
		binding.PVCCreatedAt, binding.ClaimAvailability = time.Time{}, ""
		binding.Configuration.Kind = volumecontext.EmptyDir
		binding.Configuration.MemoryBacked = true
	}
	var usage []volumecontext.RawUsage
	if in.Capacity != nil || in.Inodes != nil {
		usage = []volumecontext.RawUsage{{Namespace: scope.Namespace, PodUID: scope.PodUID, NodeUID: scope.NodeUID, VolumeName: binding.VolumeName, PVCNamespace: claimNamespace, PVCName: binding.PVCName,
			Filesystem: volumecontext.Filesystem{CapturedAt: fixtureTime.Add(-time.Duration(in.AgeSeconds) * time.Second), CapacityBytes: in.Capacity, UsedBytes: in.Used, AvailableBytes: in.Available, Inodes: in.Inodes, InodesFree: in.InodesFree}}}
	}
	var health []volumecontext.HealthObservation
	if in.Health != "unreported" {
		h := volumehealth.Observation{Identity: volumehealth.Identity{Namespace: scope.Namespace, PodName: scope.PodName, PodUID: scope.PodUID, NodeName: scope.NodeName, VolumeName: binding.VolumeName, PVCName: binding.PVCName, PVCUID: binding.PVCUID, Driver: binding.Driver}, Source: volumehealth.PodSource, Scope: volumehealth.VolumeScope, Availability: volumehealth.Reported, ObservedAt: fixtureTime}
		if in.Health != "healthy" {
			h.Conditions = []volumehealth.Condition{{Status: "Degraded"}}
		}
		health = append(health, volumecontext.HealthObservation{Observation: h, NodeUID: scope.NodeUID})
		if in.Health == "conflicting" {
			h.Source = volumehealth.ControllerSource
			h.Conditions = nil
			health = append(health, volumecontext.HealthObservation{Observation: h, NodeUID: scope.NodeUID})
		}
	}
	view, err := volumecontext.Join(scope, []volumecontext.Binding{binding}, usage, health, volumecontext.SourceState(volumehealth.Reported, ""), fixtureTime)
	if err != nil {
		return Prediction{}, err
	}
	finding := explain.AnalyzeVolumes(explain.VolumeInput{Pod: fixturePod(*c.Memory), Volumes: view.Authorised(), Now: fixtureTime})
	checks := recommend.ForVolumes(finding)
	return Prediction{Diagnosis: "storage-" + string(finding.StorageSeverity), Confidence: "not-assessed", Recommendations: checks, Abstain: len(checks) == 0}, nil
}

func predictReplica(c Case) (Prediction, error) {
	in := c.Replica
	input := replicabaseline.Input{ObservedAt: fixtureTime, Workload: changemarkers.Object{APIVersion: "apps/v1", Kind: "Deployment", Namespace: "fixture", Name: "fixture", UID: "fixture-workload"}}
	for i, value := range in.Charges {
		p := replicabaseline.Peer{Object: changemarkers.Object{APIVersion: "v1", Kind: "Pod", Namespace: "fixture", Name: fmt.Sprintf("fixture-%02d", i), UID: fmt.Sprintf("fixture-%02d", i)}, WorkloadUID: "fixture-workload", Revision: "fixture-revision", Shape: strings.Repeat("a", 64), Lifecycle: replicabaseline.Ready, SampleState: replicabaseline.Available, HistoryState: replicabaseline.Unreported, CapturedAt: fixtureTime.Add(-time.Second), StableSince: fixtureTime.Add(-10 * time.Minute), Changes: replicabaseline.ChangeState(in.ChangeHistory), Values: map[replicabaseline.Metric]replicabaseline.Value{replicabaseline.Charge: {State: replicabaseline.Available, Number: float64(value)}}}
		if i == 0 {
			switch in.ReferenceState {
			case "stale":
				p.CapturedAt = fixtureTime.Add(-time.Minute)
			case "partial":
				p.SampleState = replicabaseline.Partial
			case "revision-mismatch":
				p.Revision = "different-revision"
			}
		}
		input.Peers = append(input.Peers, p)
	}
	report, err := replicabaseline.Analyse(input)
	if err != nil {
		return Prediction{}, err
	}
	candidate := input.Peers[len(input.Peers)-1].Object.UID
	for _, peer := range report.Peers {
		if peer.Peer.UID != candidate {
			continue
		}
		for _, value := range peer.Comparisons {
			if value.Metric != replicabaseline.Charge {
				continue
			}
			diagnosis := "replica-" + value.Outlier
			if value.State != "compared" {
				diagnosis = "replica-" + value.State
			}
			return Prediction{Diagnosis: diagnosis, Confidence: value.Confidence, Abstain: value.State != "compared"}, nil
		}
	}
	return Prediction{}, fmt.Errorf("fixture produced no replica comparison")
}
