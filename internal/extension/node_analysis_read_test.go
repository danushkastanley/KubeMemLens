package extension

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/collector"
	"github.com/danushkastanley/kube-memlens/internal/model"
	"github.com/danushkastanley/kube-memlens/internal/nodeanalysis"
	"k8s.io/apiserver/pkg/authorization/authorizer"
)

func analysisHandler(t *testing.T) *ReadHandler {
	t.Helper()
	now := time.Now().UTC()
	store := collector.NewStore()
	if err := store.ReconcileNodeIdentities(map[string]string{"node-a": "node-uid-a"}, now); err != nil {
		t.Fatal(err)
	}
	observation := nodeRequest(now, 1).Snapshot.NodeContext
	if err := store.ReplaceNodeContext(*observation); err != nil {
		t.Fatal(err)
	}
	snapshot := api.AgentSnapshot{NodeName: "node-a", CapturedAt: now, Environment: api.NodeEnvironment{CgroupVersion: "v2", WorkloadContextAvailable: true},
		Containers: []api.ContainerSnapshot{{ContainerID: "private-container", Namespace: "private-tenant", PodName: "private-pod", PodUID: "private-uid", ContainerName: "app", Memory: model.MemoryBreakdown{TotalBytes: 50, AnonBytes: 25}}}}
	if _, err := store.ReplaceAuthenticatedNodeSnapshot(snapshot, "node-uid-a"); err != nil {
		t.Fatal(err)
	}
	handler := NewReadHandler(store, collector.DefaultHandlerOptions(time.Minute))
	handler.nodeContextEnabled = true
	handler.now = func() time.Time { return now }
	return handler
}

func TestNodeAnalysisAuthorisesClusterPodsBeforeLookupAndClearsRevokedData(t *testing.T) {
	handler := analysisHandler(t)
	allowed := true
	calls := 0
	handler.podAuthorizer = authorizer.AuthorizerFunc(func(_ context.Context, a authorizer.Attributes) (authorizer.Decision, string, error) {
		calls++
		if a.GetVerb() != "list" || a.GetAPIGroup() != api.MemoryAPIGroup || a.GetResource() != "pods" || a.GetNamespace() != "" || a.GetName() != "" || a.GetUser().GetName() != "tenant-reader" {
			t.Error("incorrect secondary authorisation attributes")
		}
		if allowed {
			return authorizer.DecisionAllow, "", nil
		}
		return authorizer.DecisionDeny, "", nil
	})
	first := serveNodeRead(t, handler, "/nodecontexts/node-a/analysis", "3")
	if first.Code != http.StatusOK || !strings.Contains(first.Body.String(), "private-pod") {
		t.Fatalf("authorised analysis: %d %s", first.Code, first.Body.String())
	}
	allowed = false
	second := serveNodeRead(t, handler, "/nodecontexts/node-a/analysis", "3")
	if second.Code != http.StatusOK {
		t.Fatal(second.Body.String())
	}
	for _, private := range []string{"private-tenant", "private-pod", "private-uid", "private-container", `"coverage"`, `"rankings"`, `"observedPodChargeBytes"`} {
		if strings.Contains(second.Body.String(), private) {
			t.Fatalf("revoked analysis leaked %s", private)
		}
	}
	var decoded api.NodeMemoryAnalysis
	if err := json.Unmarshal(second.Body.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Analysis.ContributorAccess != nodeanalysis.NodeOnly || calls != 2 {
		t.Fatal("permission decision was cached")
	}
}

func TestNodeAnalysisAuthorisationFailureDoesNotReadStore(t *testing.T) {
	handler := NewReadHandler(nil, collector.DefaultHandlerOptions(time.Minute))
	handler.nodeContextEnabled = true
	handler.podAuthorizer = authorizer.AuthorizerFunc(func(context.Context, authorizer.Attributes) (authorizer.Decision, string, error) {
		return authorizer.DecisionAllow, "", errors.New("private backend error")
	})
	response := serveNodeRead(t, handler, "/nodecontexts/node-a/analysis", "3")
	if response.Code != http.StatusServiceUnavailable || strings.Contains(response.Body.String(), "private backend") {
		t.Fatal("secondary authorisation failed open")
	}
}

func TestNodeAnalysisRejectsUnboundedRankAndResponse(t *testing.T) {
	handler := analysisHandler(t)
	handler.podAuthorizer = authorizer.AuthorizerFunc(func(context.Context, authorizer.Attributes) (authorizer.Decision, string, error) {
		return authorizer.DecisionAllow, "", nil
	})
	for _, query := range []string{"?limit=101", "?limit=-1", "?rank=unknown"} {
		if response := serveNodeRead(t, handler, "/nodecontexts/node-a/analysis"+query, "3"); response.Code != http.StatusBadRequest {
			t.Fatalf("invalid query accepted: %s", query)
		}
	}
	handler.opts.MaxResponseBytes = 128
	if response := serveNodeRead(t, handler, "/nodecontexts/node-a/analysis", "3"); response.Code != http.StatusInsufficientStorage {
		t.Fatal("analysis response limit ignored")
	}
}

func TestNodeAnalysisUsesOnlyOperatorQualification(t *testing.T) {
	handler := analysisHandler(t)
	handler.podAuthorizer = authorizer.AuthorizerFunc(func(context.Context, authorizer.Attributes) (authorizer.Decision, string, error) {
		return authorizer.DecisionAllow, "", nil
	})
	record, _ := handler.store.GetNodeContext("node-a", handler.now())
	handler.accounting = map[string]nodeanalysis.Qualification{record.NodeUID: {NodeUID: record.NodeUID, NodeStartedAt: record.LastGood.Stats.StartedAt,
		ValidFrom: handler.now().Add(-time.Minute), ExpiresAt: handler.now().Add(time.Hour), EvidenceSHA256: strings.Repeat("a", 64), UsageDefinition: nodeanalysis.ChargeInclusive}}
	response := serveNodeRead(t, handler, "/nodecontexts/node-a/analysis", "3")
	var result api.NodeMemoryAnalysis
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &result) != nil {
		t.Fatal(response.Body.String())
	}
	if result.Analysis.OutsidePods.Bytes == nil || *result.Analysis.OutsidePods.Bytes != 73 {
		t.Fatal("operator qualification did not reach the pure analysis")
	}
	handler.accounting = nil
	result = api.NodeMemoryAnalysis{}
	response = serveNodeRead(t, handler, "/nodecontexts/node-a/analysis?usageDefinition=cgroup-charge-inclusive&qualified=true", "3")
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &result) != nil {
		t.Fatal(response.Body.String())
	}
	if result.Analysis.OutsidePods.Bytes != nil {
		t.Fatal("read query granted accounting qualification")
	}
}
