package collector

import (
	"fmt"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/changemarkers"
	"github.com/danushkastanley/kube-memlens/internal/model"
	"github.com/danushkastanley/kube-memlens/internal/replicabaseline"
)

func replicaStoreFixture(t *testing.T) (*Store, replicabaseline.Selection, api.AgentSnapshot, time.Time) {
	t.Helper()
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	store := NewStore()
	store.EnableReplicaComparisons()
	selection := replicabaseline.Selection{Workload: changemarkers.Object{APIVersion: "apps/v1", Kind: "Deployment", Namespace: "tenant-a", Name: "app", UID: "root-uid"}, ResolvedAt: now}
	snapshot := api.AgentSnapshot{NodeName: "node-a", CapturedAt: now, Environment: api.NodeEnvironment{CgroupVersion: "v2", NodeContextAvailable: true, WorkloadContextAvailable: true}}
	for i := 0; i < 5; i++ {
		name := fmt.Sprintf("app-%d", i)
		object := changemarkers.Object{APIVersion: "v1", Kind: "Pod", Namespace: "tenant-a", Name: name, UID: name + "-uid"}
		selection.Members = append(selection.Members, replicabaseline.Member{Object: object, Node: "node-a", NodeUID: "node-uid", Revision: "rs-uid", Shape: fmt.Sprintf("%064x", 1), Lifecycle: replicabaseline.Ready, Changes: replicabaseline.ChangeUnknown, StableSince: now.Add(-time.Hour), Containers: []replicabaseline.Container{{Name: "app", Role: "container", ID: name, StartedAt: now.Add(-time.Hour)}}})
		total := uint64(32 << 20)
		if i == 4 {
			total = 96 << 20
		}
		snapshot.Containers = append(snapshot.Containers, api.ContainerSnapshot{Namespace: object.Namespace, PodName: name, PodUID: object.UID, ContainerName: "app", ContainerID: name, CgroupPath: "/pods/" + name, Memory: model.MemoryBreakdown{TotalBytes: total, AnonBytes: total, PressureKnown: true, SwapCurrentKnown: true, LocalEventsKnown: true}})
	}
	for i := 0; i <= 72; i++ {
		snapshot.CapturedAt = now.Add(-6*time.Minute + time.Duration(i)*5*time.Second)
		if _, err := store.ReplaceAuthenticatedNodeSnapshot(snapshot, "node-uid"); err != nil {
			t.Fatal(err)
		}
	}
	return store, selection, snapshot, now
}

func TestReplicaStoreProjectsActualSamplesAndControlledOutlier(t *testing.T) {
	store, selection, _, now := replicaStoreFixture(t)
	input, err := store.ReplicaEvidence(t.Context(), selection, now)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range input.Peers {
		if p.SampleState != replicabaseline.Available || len(p.History) != 73 || !p.DeltaStartedAt.Equal(now.Add(-5*time.Second)) {
			t.Fatalf("projection: %+v", p)
		}
	}
	report, err := replicabaseline.Analyse(input)
	if err != nil {
		t.Fatal(err)
	}
	candidate := report.Peers[4]
	found := false
	for _, c := range candidate.Comparisons {
		if c.Metric == replicabaseline.Charge {
			found = true
			if c.Outlier != "higher" || len(c.References) != 4 || c.Confidence != "limited-change-history" {
				t.Fatalf("outlier: %+v", c)
			}
		}
		if c.Metric == replicabaseline.ChargeSlope && c.State != "compared" {
			t.Fatalf("history: %+v", c)
		}
	}
	if !found {
		t.Fatal("charge comparison missing")
	}
	// The returned history is a copy; consumers cannot mutate retained evidence.
	input.Peers[0].History[0].Bytes = 999
	again, err := store.ReplicaEvidence(t.Context(), selection, now)
	if err != nil || again.Peers[0].History[0].Bytes == 999 {
		t.Fatal("store evidence was aliased", err)
	}
}

func TestReplicaStoreRejectsWrongOrIncompleteCurrentIdentity(t *testing.T) {
	for _, change := range []string{"node replaced", "pod replaced", "container replaced", "container missing", "extra contributor", "partial", "stale", "resource mismatch", "node read error"} {
		t.Run(change, func(t *testing.T) {
			store, selection, snapshot, now := replicaStoreFixture(t)
			switch change {
			case "node replaced":
				selection.Members[0].NodeUID = "new-node"
			case "pod replaced":
				selection.Members[0].Object.UID = "new-pod"
			case "container replaced":
				selection.Members[0].Containers[0].ID = "replacement"
			case "container missing":
				snapshot.Containers = snapshot.Containers[1:]
			case "extra contributor":
				extra := snapshot.Containers[0]
				extra.ContainerName = "sidecar"
				extra.ContainerID = "sidecar"
				snapshot.Containers = append(snapshot.Containers, extra)
			case "node read error":
				snapshot.Environment.CgroupReadErrors = 1
			case "partial":
				snapshot.Containers[0].Completeness = api.EvidencePartial
			case "stale":
				now = now.Add(time.Minute)
			case "resource mismatch":
				selection.Members[0].Containers[0].Configured.Limit = model.ResourceValue{Known: true, Bytes: 64 << 20}
			}
			if change != "stale" {
				snapshot.CapturedAt = now.Add(time.Second)
				now = snapshot.CapturedAt
				if _, err := store.ReplaceAuthenticatedNodeSnapshot(snapshot, "node-uid"); err != nil {
					t.Fatal(err)
				}
			}
			input, err := store.ReplicaEvidence(t.Context(), selection, now)
			if err != nil {
				t.Fatal(err)
			}
			if input.Peers[0].SampleState == replicabaseline.Available {
				t.Fatal("invalid current evidence available")
			}
			report, err := replicabaseline.Analyse(input)
			if err != nil {
				t.Fatal(err)
			}
			if report.Peers[0].Exclusion == "" {
				t.Fatal("invalid peer entered baseline")
			}
		})
	}
}

func TestReplicaHistoryResetsForContributorOrEnforcementChange(t *testing.T) {
	for _, change := range []string{"container", "limit", "partial", "node"} {
		t.Run(change, func(t *testing.T) {
			store, selection, snapshot, now := replicaStoreFixture(t)
			uid := "node-uid"
			snapshot.CapturedAt = now.Add(5 * time.Second)
			switch change {
			case "container":
				snapshot.Containers[0].ContainerID = "new"
				selection.Members[0].Containers[0].ID = "new"
			case "limit":
				snapshot.Containers[0].Memory.MaxKnown = true
				snapshot.Containers[0].Memory.MaxBytes = 128 << 20
			case "partial":
				snapshot.Containers[0].Completeness = api.EvidencePartial
			case "node":
				uid = "replacement-node"
				selection.Members[0].NodeUID = uid
			}
			if _, err := store.ReplaceAuthenticatedNodeSnapshot(snapshot, uid); err != nil {
				t.Fatal(err)
			}
			snapshot.CapturedAt = snapshot.CapturedAt.Add(5 * time.Second)
			snapshot.Containers[0].Completeness = api.EvidenceComplete
			if _, err := store.ReplaceAuthenticatedNodeSnapshot(snapshot, uid); err != nil {
				t.Fatal(err)
			}
			input, err := store.ReplicaEvidence(t.Context(), selection, snapshot.CapturedAt)
			if err != nil {
				t.Fatal(err)
			}
			p := input.Peers[0]
			if !p.StableSince.After(now) || len(p.History) > 2 {
				t.Fatalf("prior contributors leaked into current history: %+v", p)
			}
			report, err := replicabaseline.Analyse(input)
			if err != nil {
				t.Fatal(err)
			}
			if report.Peers[0].Exclusion == "" {
				t.Fatal("recently changed history became baseline")
			}
		})
	}
}
