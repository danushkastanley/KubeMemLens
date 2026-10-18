package client

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/incidentsession"
	"github.com/danushkastanley/kube-memlens/internal/incidentsessionapi"
	"github.com/danushkastanley/kube-memlens/internal/memoryhistory"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apiserver/pkg/authentication/user"
	"k8s.io/apiserver/pkg/authorization/authorizer"
	apirequest "k8s.io/apiserver/pkg/endpoints/request"
	"k8s.io/client-go/rest"
)

type incidentNamespaceFixture struct{}

func (incidentNamespaceFixture) Lookup(context.Context, string) (string, error) {
	return "namespace-uid", nil
}

type incidentEvidenceFixture struct{}

func (incidentEvidenceFixture) CapturePod(_ context.Context, p incidentsession.Principal, name string) ([]byte, error) {
	now := time.Now().UTC()
	return json.Marshal(api.IncidentBundle{SchemaVersion: 1, CapturedAt: now, ToolVersion: "test", Pods: []api.PodSnapshot{{Namespace: p.Namespace, PodName: name, PodUID: "pod-uid", CapturedAt: now}}})
}
func (incidentEvidenceFixture) CaptureMarkers(context.Context, incidentsession.Principal, string, memoryhistory.Query) ([]byte, error) {
	return nil, incidentsession.ErrDisabled
}

func incidentTLSClient(t *testing.T, handler http.Handler) (*IncidentClient, *rest.Config) {
	t.Helper()
	server := httptest.NewTLSServer(handler)
	t.Cleanup(server.Close)
	scope, err := NamespaceScope("team-a")
	if err != nil {
		t.Fatal(err)
	}
	config := &rest.Config{Host: server.URL, TLSClientConfig: rest.TLSClientConfig{CAData: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})}, BearerToken: "fixture-token"}
	client, err := NewIncidentSessionClient(config, scope)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	return client, config
}

func incidentAPI(t *testing.T) http.Handler {
	t.Helper()
	authority, err := incidentsessionapi.NewAuthority([]string{"team-a"}, incidentNamespaceFixture{}, authorizer.AuthorizerFunc(func(context.Context, authorizer.Attributes) (authorizer.Decision, string, error) {
		return authorizer.DecisionAllow, "", nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	handler, err := incidentsessionapi.NewHandler(context.Background(), authority, incidentsession.DefaultLimits(), incidentEvidenceFixture{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(handler.Shutdown)
	factory := apirequest.RequestInfoFactory{APIPrefixes: sets.NewString("api", "apis"), GrouplessAPIPrefixes: sets.NewString("api")}
	// Component fixture only: production authentication is performed by Kubernetes.
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fixture-token" {
			t.Error("configured credentials were not used")
		}
		info, err := factory.NewRequestInfo(r)
		if err != nil {
			t.Error(err)
			return
		}
		ctx := apirequest.WithRequestInfo(r.Context(), info)
		ctx = apirequest.WithUser(ctx, &user.DefaultInfo{Name: "operator", UID: "operator-uid", Groups: []string{user.AllAuthenticated}})
		handler.ServeHTTP(w, r.WithContext(ctx))
	})
}

func TestIncidentClientWorkflow(t *testing.T) {
	client, _ := incidentTLSClient(t, incidentAPI(t))
	ctx := context.Background()
	session, err := client.StartIncident(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !validIncidentID(session.ID) || session.Latest.Kind != incidentsession.Opened {
		t.Fatal("invalid start response")
	}
	if _, err := client.AnnotateIncident(ctx, session.ID, "private decision"); err != nil {
		t.Fatal(err)
	}
	captured, err := client.CaptureIncident(ctx, session.ID, "app")
	if err != nil || captured.Latest.Kind != incidentsession.Captured {
		t.Fatal("capture failed", err)
	}
	full, err := client.ExportIncident(ctx, session.ID, incidentsession.ExportAuthorised)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := incidentsession.DecodeExport(full)
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Authorised.Captures) != 1 {
		t.Fatal("capture bytes unavailable")
	}
	digest := doc.Authorised.Captures[0].Digest
	if _, err := client.CompareIncident(ctx, session.ID, digest, digest); err != nil {
		t.Fatal(err)
	}
	closed, err := client.CloseIncident(ctx, session.ID)
	if err != nil || closed.ClosedAt == nil {
		t.Fatal("close failed", err)
	}
	public, err := client.ExportIncident(ctx, session.ID, incidentsession.ExportSanitised)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(public), "private decision") || strings.Contains(string(public), "team-a") {
		t.Fatal("private export substituted for sanitised request")
	}
	if err := client.DeleteIncident(ctx, session.ID); err != nil {
		t.Fatal(err)
	}
}

func TestIncidentClientMarkerGapAndInspection(t *testing.T) {
	client, _ := incidentTLSClient(t, incidentAPI(t))
	ctx := context.Background()
	session, err := client.StartIncident(ctx)
	if err != nil {
		t.Fatal(err)
	}
	marked, err := client.MarkIncident(ctx, session.ID, "app", memoryhistory.Query{})
	if err != nil || marked.Latest.Kind != incidentsession.Gap || marked.Latest.GapReason != "source-disabled" {
		t.Fatal("marker failure hidden", err)
	}
	inspected, err := client.InspectIncident(ctx, session.ID)
	if err != nil || inspected.Latest != marked.Latest {
		t.Fatal("inspection lost marker state", err)
	}
	if err := client.DeleteIncident(ctx, session.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := client.InspectIncident(ctx, session.ID); err == nil {
		t.Fatal("deleted session returned")
	}
}
