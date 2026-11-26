package extension_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/agent"
	"github.com/danushkastanley/kube-memlens/internal/client"
	"github.com/danushkastanley/kube-memlens/internal/collector"
	"github.com/danushkastanley/kube-memlens/internal/extension"
	"github.com/danushkastanley/kube-memlens/internal/kube"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apiserver/pkg/authentication/user"
	apirequest "k8s.io/apiserver/pkg/endpoints/request"
	"k8s.io/client-go/rest"
)

const (
	pipelineAgentUsername = "system:serviceaccount:kube-memlens:kube-memlens-agent"
	pipelineAgentToken    = "agent-token"
	pipelineReaderToken   = "reader-token"
)

// TestPipelineCarriesCgroupTreeToClient runs the agent scanner, the agent
// publisher, the extension ingest handler, the collector store, the read
// handler and the API client in one process, over a cgroup tree on disk. Only
// the Kubernetes API server is stood in for: it would list the node's Pods,
// and in front of the extension it would authenticate each caller and resolve
// the request attributes.
func TestPipelineCarriesCgroupTreeToClient(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	fixture := newPipelineFixture()
	fixture.write(t, root)
	pipeline := startPipeline(t)
	scanner := &agent.Scanner{CgroupRoot: root, NodeName: pipelineNodeName}

	pipeline.scanAndPublish(t, ctx, scanner, fixture)
	assertClientReadsFixture(t, ctx, pipeline.reader, fixture)

	fixture.removePod(t, root, "batch", "report")
	fixture.setUsage(t, root, "shop", "web-1", "app", 410<<20, 320<<20)
	pipeline.scanAndPublish(t, ctx, scanner, fixture)
	assertClientReadsFixture(t, ctx, pipeline.reader, fixture)
}

type pipeline struct {
	publisher *agent.SnapshotPublisher
	reader    *client.KubernetesAPIClient
}

func startPipeline(t *testing.T) pipeline {
	t.Helper()
	// The limits the collector runs with by default, so the agent's snapshot is
	// judged by the same validation it meets in a cluster.
	handlerOptions := collector.DefaultHandlerOptions(time.Minute)
	coordinator, err := extension.NewCoordinator(collector.NewStore(), extension.CoordinatorOptions{
		Handler: handlerOptions, MaxAgents: 8, MaxRetired: 32,
	})
	if err != nil {
		t.Fatalf("NewCoordinator: %v", err)
	}
	handler, err := extension.NewHandler(coordinator, extension.HandlerOptions{
		AgentUsername: pipelineAgentUsername, MaxSnapshotBytes: handlerOptions.MaxSnapshotBytes,
		MaxConcurrent: 4, RequestsPerSec: 1, Burst: 2, MaxIdentities: 8,
	})
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}
	mux := pipelineMux{ServeMux: http.NewServeMux()}
	handler.Register(mux)
	server := httptest.NewServer(apiServerFront{next: mux, identities: map[string]user.Info{
		pipelineAgentToken: &user.DefaultInfo{
			Name: pipelineAgentUsername,
			Extra: map[string][]string{
				extension.PodUIDExtra:       {"7b2c1d40-5e6f-4a7b-8c9d-0e1f2a3b4c5d"},
				extension.NodeNameExtra:     {pipelineNodeName},
				extension.NodeUIDExtra:      {pipelineNodeUID},
				extension.CredentialIDExtra: {"credential-a"},
			},
		},
		pipelineReaderToken: &user.DefaultInfo{Name: "tenant-reader", Groups: []string{"system:authenticated"}},
	}})
	t.Cleanup(server.Close)

	publisher, err := agent.NewSnapshotPublisher(&rest.Config{Host: server.URL, BearerToken: pipelineAgentToken})
	if err != nil {
		t.Fatalf("NewSnapshotPublisher: %v", err)
	}
	reader, err := client.NewKubernetesAPIClient(&rest.Config{Host: server.URL, BearerToken: pipelineReaderToken}, client.AllNamespacesScope(), 5*time.Second)
	if err != nil {
		t.Fatalf("NewKubernetesAPIClient: %v", err)
	}
	return pipeline{publisher: publisher, reader: reader}
}

// scanAndPublish is one agent cycle: index the Pods the API server reports,
// scan the tree and post the snapshot. A snapshot the collector's validation
// refuses fails here, which is how a limit the agent does not apply before
// posting would show up in a cluster: the node stops publishing.
func (p pipeline) scanAndPublish(t *testing.T, ctx context.Context, scanner *agent.Scanner, fixture *pipelineFixture) {
	t.Helper()
	result, err := scanner.Scan(ctx, kube.BuildPodIndexFromPods(fixture.apiPods()))
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if result.WalkError != nil {
		t.Fatalf("Scan walk error: %v", result.WalkError)
	}
	if err := p.publisher.Publish(ctx, pipelineNodeUID, result.Snapshot); err != nil {
		t.Fatalf("the collector refused the agent's snapshot of the fixture tree: %v", err)
	}
}

// pipelineMux gives Register the prefix routing the generic API server's mux
// provides.
type pipelineMux struct{ *http.ServeMux }

func (m pipelineMux) HandlePrefix(path string, handler http.Handler) { m.Handle(path, handler) }

// apiServerFront stands in for the Kubernetes API server and the delegated
// authentication in front of the extension: it maps a bearer token to the
// identity the API server would forward, and resolves the request attributes
// with the same factory the generic API server uses. Authorisation is allowed
// for both identities; the ingest handler still checks the agent's claims.
type apiServerFront struct {
	next       http.Handler
	identities map[string]user.Info
}

func (f apiServerFront) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	identity, ok := f.identities[strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")]
	if !ok {
		http.Error(w, "unauthorised", http.StatusUnauthorized)
		return
	}
	factory := &apirequest.RequestInfoFactory{APIPrefixes: sets.NewString("api", "apis"), GrouplessAPIPrefixes: sets.NewString("api")}
	info, err := factory.NewRequestInfo(r)
	if err != nil {
		http.Error(w, "request attributes are unavailable", http.StatusBadRequest)
		return
	}
	ctx := apirequest.WithRequestInfo(apirequest.WithUser(r.Context(), identity), info)
	f.next.ServeHTTP(w, r.WithContext(ctx))
}
