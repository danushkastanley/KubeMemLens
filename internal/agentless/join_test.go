package agentless

import (
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/capability"
	"github.com/danushkastanley/kube-memlens/internal/observation"
	"github.com/danushkastanley/kube-memlens/internal/resourcemetrics"
)

func TestJoinRetainsPendingRowsAndExplicitMissingMetrics(t *testing.T) {
	now := time.Now().UTC()
	pod, err := podMetadata(t.Context(), pendingPod("team-a", "app"), now)
	if err != nil {
		t.Fatal(err)
	}
	pods := []observation.Pod{pod}
	joinPodMetrics(pods, resourcemetrics.Report{Availability: resourcemetrics.Available, APIVersion: "metrics.k8s.io/v1"}, now)
	if len(pods[0].Containers) != 1 || pods[0].Containers[0].State != "unreported" || pods[0].WorkingSet.Bytes != nil || pods[0].WorkingSet.Coverage.Expected != 1 || pods[0].Context.Phase != "Pending" {
		t.Fatalf("%+v", pods[0])
	}
	if pods[0].Cgroup != nil {
		t.Fatal("pending Pod acquired fabricated cgroup evidence")
	}
}

func TestJoinBindsUIDAndCurrentContainerStart(t *testing.T) {
	now := time.Now().UTC()
	pod, err := podMetadata(t.Context(), pendingPod("team-a", "app"), now)
	if err != nil {
		t.Fatal(err)
	}
	pod.Containers[0].StartedAt = now.Add(-10 * time.Second)
	for _, test := range []struct {
		uid         string
		at          time.Time
		wantValue   bool
		wantPartial bool
	}{
		{pod.UID, now, true, false}, {"", now, true, true}, {"replaced", now, false, true},
		{pod.UID, now.Add(-time.Hour), false, true}, {pod.UID, now.Add(-20 * time.Second), false, true},
	} {
		pods := []observation.Pod{pod}
		pods[0].Containers = append([]observation.Container(nil), pod.Containers...)
		value := resourcemetrics.Observation{Identity: resourcemetrics.Identity{Namespace: "team-a", PodName: "app", PodUID: test.uid, ContainerName: "worker"},
			APIVersion: "metrics.k8s.io/v1", Timestamp: test.at, Window: 15 * time.Second, Freshness: resourcemetrics.Fresh, MemoryWorkingSetBytes: 32 << 20}
		joinPodMetrics(pods, resourcemetrics.Report{Availability: resourcemetrics.Available, Observations: []resourcemetrics.Observation{value}}, now)
		got := pods[0].Containers[0].WorkingSet
		if (got.Bytes != nil) != test.wantValue || (got.Evidence.Completeness == capability.Partial) != test.wantPartial {
			t.Fatalf("uid=%q at=%v got=%+v", test.uid, test.at, got)
		}
		if got.Bytes != nil && (*got.Bytes != 32<<20 || got.Evidence.Source != capability.KubernetesMetrics || got.Evidence.CapturedAt != test.at) {
			t.Fatalf("%+v", got)
		}
	}
}

func TestJoinNeverUsesAnotherNamespacesSameName(t *testing.T) {
	now := time.Now().UTC()
	pod, err := podMetadata(t.Context(), pendingPod("team-a", "app"), now)
	if err != nil {
		t.Fatal(err)
	}
	pods := []observation.Pod{pod}
	joinPodMetrics(pods, resourcemetrics.Report{Availability: resourcemetrics.Available, Observations: []resourcemetrics.Observation{{
		Identity: resourcemetrics.Identity{Namespace: "team-b", PodName: "app", ContainerName: "worker"}, Timestamp: now, MemoryWorkingSetBytes: 99,
	}}}, now)
	if pods[0].WorkingSet.Bytes != nil {
		t.Fatal("cross-namespace join")
	}
}
