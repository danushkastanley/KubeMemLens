package extension

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/collector"
	"github.com/danushkastanley/kube-memlens/internal/memorytopology"
	"k8s.io/apiserver/pkg/authentication/user"
	"k8s.io/apiserver/pkg/authorization/authorizer"
)

func topologyRequest(t *testing.T, now time.Time) api.NodeSnapshotRequest {
	t.Helper()
	r := nodeRequest(now, 1)
	r.Snapshot.SchemaVersion = 7
	o := memorytopology.Failure("node-a", "node-uid-a", now)
	raw, err := json.Marshal(o)
	if err != nil {
		t.Fatal(err)
	}
	r.Snapshot.Topology = raw
	return r
}
func TestTopologyProducerScopeAndAtomicRejection(t *testing.T) {
	now := time.Now().UTC()
	store := collector.NewStore()
	if err := store.ReconcileNodeIdentities(map[string]string{"node-a": "node-uid-a"}, now); err != nil {
		t.Fatal(err)
	}
	c := testCoordinator(t, store, now, 4)
	claims := testClaims("node-producer", "node-a", "node-uid-a")
	r := topologyRequest(t, now)
	_, _, err := c.Accept(claims, r)
	assertIngestionCode(t, err, "producer_scope")
	claims.Role = NodeContextProducer
	r.Snapshot.SchemaVersion = 6
	_, _, err = c.Accept(claims, r)
	assertIngestionCode(t, err, "invalid_snapshot")
	r.Snapshot.SchemaVersion = 7
	r.Snapshot.Topology = json.RawMessage(`{"schemaVersion":1,"nodeName":"other-node"}`)
	_, _, err = c.Accept(claims, r)
	assertIngestionCode(t, err, "invalid_snapshot")
	if c.Epoch(claims.instanceKey()).LastSequence != 0 || store.NodeContextDebug(now).HistoryPoints != 0 {
		t.Fatal("invalid topology advanced state")
	}
	r = topologyRequest(t, now)
	body, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if err := boundedNodeWire(body); err != nil {
		t.Fatal(err)
	}
	if _, _, err = c.Accept(claims, r); err != nil {
		t.Fatal(err)
	}
}
func TestTopologyReadProfileSchemaAndInventory(t *testing.T) {
	now := time.Now().UTC()
	store := collector.NewStore()
	if err := store.ReconcileNodeIdentities(map[string]string{"node-a": "node-uid-a"}, now); err != nil {
		t.Fatal(err)
	}
	r := topologyRequest(t, now)
	if err := store.ReplaceNodeEvidence(*r.Snapshot.NodeContext, nil, r.Snapshot.Topology); err != nil {
		t.Fatal(err)
	}
	h := NewReadHandler(store, collector.DefaultHandlerOptions(time.Minute))
	h.now = func() time.Time { return now }
	h.nodeContextEnabled = true
	path := "/nodecontexts/node-a/topology"
	if got := serveNodeRead(t, h, path, "7"); got.Code != 404 {
		t.Fatal("disabled profile readable")
	}
	h.topologyEnabled = true
	if got := serveNodeRead(t, h, path, "6"); got.Code != 404 {
		t.Fatal("old schema readable")
	}
	response := serveNodeRead(t, h, path, "7")
	if response.Code != http.StatusOK {
		t.Fatalf("%d %s", response.Code, response.Body.String())
	}
	var result api.NodeMemoryTopology
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Name != "node-a" || result.Kind != "NodeMemoryTopology" || result.Current == nil || result.Current.Validate() != nil {
		t.Fatal("invalid topology response")
	}
	h.opts.MaxResponseBytes = 128
	if got := serveNodeRead(t, h, path, "7"); got.Code != 507 {
		t.Fatal("response budget bypass")
	}
	now = now.Add(time.Minute)
	if got := serveNodeRead(t, h, path, "7"); got.Code != 503 {
		t.Fatal("expired inventory exposed topology")
	}
}
func TestTopologyAuthorizerDelegatesNamedNodeOnly(t *testing.T) {
	calls := 0
	gate := agentIdentityAuthorizer{delegate: authorizer.AuthorizerFunc(func(_ context.Context, a authorizer.Attributes) (authorizer.Decision, string, error) {
		calls++
		if a.GetResource() != "nodecontexts" || a.GetSubresource() != "topology" || a.GetName() != "node-a" || a.GetNamespace() != "" || a.GetVerb() != "get" {
			t.Fatal("topology scope changed")
		}
		return authorizer.DecisionDeny, "", nil
	})}
	decision, _, err := gate.Authorize(context.Background(), authorizer.AttributesRecord{User: &user.DefaultInfo{Name: "node-viewer"}, ResourceRequest: true, APIGroup: api.MemoryAPIGroup, APIVersion: api.MemoryAPIVersion, Resource: "nodecontexts", Subresource: "topology", Name: "node-a", Verb: "get"})
	if err != nil || decision != authorizer.DecisionDeny || calls != 1 {
		t.Fatal("Node authorisation bypass")
	}
}
