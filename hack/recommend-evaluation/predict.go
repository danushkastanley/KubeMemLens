package main

import (
	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/explain"
	"github.com/danushkastanley/kube-memlens/internal/model"
	"github.com/danushkastanley/kube-memlens/internal/recommend"
	"time"
)

var fixtureTime = time.Date(2026, 9, 29, 0, 0, 0, 0, time.UTC)

func memoryValue(in Memory) model.MemoryBreakdown {
	m := model.MemoryBreakdown{TotalBytes: in.Charge, AnonBytes: in.Anon, FileBytes: in.File,
		ShmemBytes: in.Shmem, SlabBytes: in.Slab, SlabUnreclaimableBytes: in.SlabUnreclaimable, DirtyBytes: in.Dirty, WritebackBytes: in.Writeback,
		SocketBytes: in.Socket, PageTableBytes: in.PageTables}
	for _, c := range []struct {
		value *uint64
		bytes *uint64
		known *bool
	}{
		{in.Min, &m.MinBytes, &m.MinKnown}, {in.Low, &m.LowBytes, &m.LowKnown},
		{in.High, &m.HighBytes, &m.HighKnown}, {in.Max, &m.MaxBytes, &m.MaxKnown},
	} {
		if c.value != nil {
			*c.bytes, *c.known = *c.value, true
		}
	}
	m.LocalEventsKnown = in.LocalEvents
	if in.LocalEvents {
		m.LocalEventDeltasKnown = in.DeltasKnown
		m.LocalOOMEvents, m.LocalOOMKillEvents, m.LocalMaxEvents, m.LocalHighEvents = in.OOM, in.OOMKill, in.MaxEvents, in.HighEvents
		m.LocalOOMEventsDelta, m.LocalOOMKillEventsDelta, m.LocalMaxEventsDelta, m.LocalHighEventsDelta = in.OOM, in.OOMKill, in.MaxEvents, in.HighEvents
	} else {
		m.EventDeltasKnown = in.DeltasKnown
		m.OOMEvents, m.OOMKillEvents, m.MaxEvents, m.HighEvents = in.OOM, in.OOMKill, in.MaxEvents, in.HighEvents
		m.OOMEventsDelta, m.OOMKillEventsDelta, m.MaxEventsDelta, m.HighEventsDelta = in.OOM, in.OOMKill, in.MaxEvents, in.HighEvents
	}
	if in.PSISome != nil && in.PSIFull != nil {
		m.PressureKnown = true
		m.PSISomeAvg10, m.PSIFullAvg10 = *in.PSISome, *in.PSIFull
	}
	return m
}

func fixturePod(in Memory) api.PodSnapshot {
	m := memoryValue(in)
	c := api.ContainerSnapshot{ContainerName: "fixture", ContainerID: "fixture-runtime", Memory: m, CapturedAt: fixtureTime,
		DeltaWindowKnown: in.DeltasKnown, DeltaStartedAt: fixtureTime.Add(-5 * time.Second)}
	if in.Stale {
		c.Freshness = api.EvidenceFreshnessStale
	}
	pod := api.PodSnapshot{Namespace: "fixture", PodName: "fixture", PodUID: "fixture-pod", NodeName: "fixture-node",
		CapturedAt: fixtureTime, Memory: m, Containers: []api.ContainerSnapshot{c}, Freshness: c.Freshness}
	if in.TerminationAgeSeconds != nil {
		pod.Context.LastTerminationKnown = true
		pod.Context.LastTerminationReason = "OOMKilled"
		pod.Context.LastTerminationFinishedAt = fixtureTime.Add(-time.Duration(*in.TerminationAgeSeconds) * time.Second)
	}
	return pod
}

func predict(c Case) (Prediction, error) {
	switch c.Family {
	case "memory":
		finding := explain.AnalyzePodAt(fixturePod(*c.Memory), fixtureTime)
		return Prediction{Diagnosis: string(finding.Diagnosis), Confidence: string(finding.Confidence),
			Recommendations: recommend.ForFinding(finding), Abstain: finding.Diagnosis == explain.DiagnosisNormal || finding.Diagnosis == explain.DiagnosisMixed}, nil
	case "qos":
		pod := fixturePod(*c.Memory)
		finding := explain.InterpretMemoryQoS(pod.Containers[0])
		checks := recommend.ForPodMemoryQoS([]api.PodSnapshot{pod})
		return Prediction{Diagnosis: string(finding.State), Confidence: string(finding.Confidence), Recommendations: checks, Abstain: len(checks) == 0}, nil
	case "volume":
		return predictVolume(c)
	default:
		return predictReplica(c)
	}
}
