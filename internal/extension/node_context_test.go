package extension

import (
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/capability"
	"github.com/danushkastanley/kube-memlens/internal/collector"
	"github.com/danushkastanley/kube-memlens/internal/nodecontext"
	"k8s.io/apiserver/pkg/authentication/user"
)

func nodeRequest(now time.Time, sequence uint64) api.NodeSnapshotRequest {
	request := testRequest(now, "epoch-a", sequence, "node-a", "node-uid-a")
	request.Snapshot.Environment = api.NodeEnvironment{}
	request.Snapshot.Containers = nil
	request.Snapshot.SchemaVersion = 3
	usage := uint64(123)
	request.Snapshot.NodeContext = &nodecontext.Observation{NodeName: "node-a", NodeUID: "node-uid-a", ReportedAt: now, Availability: capability.Available,
		Evidence: capability.Envelope{Source: nodecontext.Source, APIVersion: "v1alpha1", CapturedAt: now, ReceivedAt: now,
			Scope: capability.NodeScope, Freshness: capability.Fresh, Completeness: capability.Partial, Stability: capability.ImplementationSpecific},
		Stats: &nodecontext.Stats{StartedAt: now.Add(-time.Hour), Provenance: nodecontext.Unknown, Memory: &nodecontext.Memory{CapturedAt: now, UsageBytes: &usage}}}
	return request
}

func TestNodeProducerDoesNotRetireOrOverwriteCgroupProducer(t *testing.T) {
	now := time.Now().UTC()
	store := collector.NewStore()
	if err := store.ReconcileNodeIdentities(map[string]string{"node-a": "node-uid-a"}, now); err != nil {
		t.Fatal(err)
	}
	coordinator := testCoordinator(t, store, now, 4)
	cgroup := testClaims("cgroup-pod", "node-a", "node-uid-a")
	node := testClaims("node-pod", "node-a", "node-uid-a")
	node.Role = NodeContextProducer
	if _, _, err := coordinator.Accept(cgroup, testRequest(now, "epoch-a", 1, "node-a", "node-uid-a")); err != nil {
		t.Fatal(err)
	}
	request := nodeRequest(now, 1)
	if _, _, err := coordinator.Accept(node, request); err != nil {
		t.Fatal(err)
	}
	if len(store.ListContainers(now, time.Minute)) != 1 {
		t.Fatal("Node producer cleared containers")
	}
	if _, duplicate, err := coordinator.Accept(node, request); err != nil || !duplicate {
		t.Fatal("Node duplicate failed", err)
	}
	replacement := node
	replacement.PodUID = "node-pod-new"
	if _, _, err := coordinator.Accept(replacement, nodeRequest(now.Add(time.Second), 1)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := coordinator.Accept(cgroup, testRequest(now.Add(time.Second), "epoch-a", 2, "node-a", "node-uid-a")); err != nil {
		t.Fatal("Node replacement retired cgroup producer", err)
	}
	_, _, err := coordinator.Accept(node, nodeRequest(now.Add(2*time.Second), 2))
	assertIngestionCode(t, err, "agent_replaced")
	if coordinator.Epoch(cgroup.instanceKey()).LastSequence != 2 || coordinator.Epoch(replacement.instanceKey()).LastSequence != 1 {
		t.Fatal("producer sequence state was shared")
	}
	if store.NodeContextDebug(now).HistoryPoints != 2 {
		t.Fatal("duplicate post added a history point")
	}
}

func TestCrossRoleSnapshotRejectionDoesNotAdvanceState(t *testing.T) {
	now := time.Now().UTC()
	store := collector.NewStore()
	if err := store.ReconcileNodeIdentities(map[string]string{"node-a": "node-uid-a"}, now); err != nil {
		t.Fatal(err)
	}
	coordinator := testCoordinator(t, store, now, 4)
	claims := testClaims("producer", "node-a", "node-uid-a")
	_, _, err := coordinator.Accept(claims, nodeRequest(now, 1))
	assertIngestionCode(t, err, "producer_scope")
	claims.Role = NodeContextProducer
	_, _, err = coordinator.Accept(claims, testRequest(now, "epoch-a", 1, "node-a", "node-uid-a"))
	assertIngestionCode(t, err, "producer_scope")
	forged := nodeRequest(now, 1)
	forged.Snapshot.NodeContext.NodeUID = "other-node"
	_, _, err = coordinator.Accept(claims, forged)
	assertIngestionCode(t, err, "producer_scope")
	if coordinator.Epoch(claims.instanceKey()).LastSequence != 0 {
		t.Fatal("rejected payload advanced sequence")
	}
	if _, _, err := coordinator.Accept(claims, nodeRequest(now, 1)); err != nil {
		t.Fatal(err)
	}
}

func TestProducerRoleComesFromConfiguredAccount(t *testing.T) {
	info := &user.DefaultInfo{Name: "node-account", Extra: map[string][]string{
		PodUIDExtra: {"pod-a"}, NodeNameExtra: {"node-a"}, NodeUIDExtra: {"uid-a"}, CredentialIDExtra: {"cred-a"}}}
	if _, err := producerClaims(info, "cgroup-account", ""); err == nil {
		t.Fatal("disabled producer admitted")
	}
	claims, err := producerClaims(info, "cgroup-account", "node-account")
	if err != nil || claims.Role != NodeContextProducer {
		t.Fatal("configured producer denied", err)
	}
	info.Name = "cgroup-account"
	claims, err = producerClaims(info, "cgroup-account", "node-account")
	if err != nil || claims.Role != CgroupProducer {
		t.Fatal("cgroup identity changed", err)
	}
}

func TestNodeReplacementReleasesAdmissionCapacityForBothSources(t *testing.T) {
	now := time.Now().UTC()
	store := collector.NewStore()
	if err := store.ReconcileNodeIdentities(map[string]string{"node-a": "node-uid-a"}, now); err != nil {
		t.Fatal(err)
	}
	coordinator := testCoordinator(t, store, now, 2)
	old := testClaims("old-cgroup", "node-a", "node-uid-a")
	if _, _, err := coordinator.Accept(old, testRequest(now, "epoch-a", 1, "node-a", "node-uid-a")); err != nil {
		t.Fatal(err)
	}
	oldNode := testClaims("old-node-context", "node-a", "node-uid-a")
	oldNode.Role = NodeContextProducer
	if _, _, err := coordinator.Accept(oldNode, nodeRequest(now, 1)); err != nil {
		t.Fatal(err)
	}
	if err := store.ReconcileNodeIdentities(map[string]string{"node-b": "node-uid-b"}, now); err != nil {
		t.Fatal(err)
	}
	current := testClaims("new-cgroup", "node-b", "node-uid-b")
	if _, _, err := coordinator.Accept(current, testRequest(now, "epoch-a", 1, "node-b", "node-uid-b")); err != nil {
		t.Fatal("retired Node held capacity", err)
	}
	currentNode := testClaims("new-node-context", "node-b", "node-uid-b")
	currentNode.Role = NodeContextProducer
	request := nodeRequest(now, 1)
	request.NodeUID, request.Snapshot.NodeName = "node-uid-b", "node-b"
	request.Snapshot.NodeContext.NodeUID, request.Snapshot.NodeContext.NodeName = "node-uid-b", "node-b"
	if _, _, err := coordinator.Accept(currentNode, request); err != nil {
		t.Fatal(err)
	}
	_, _, err := coordinator.Accept(old, testRequest(now, "epoch-a", 2, "node-a", "node-uid-a"))
	assertIngestionCode(t, err, "agent_replaced")
	if len(coordinator.agents) != 2 || len(coordinator.retired) != 2 {
		t.Fatal("identity bounds changed")
	}
}
