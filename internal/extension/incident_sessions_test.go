package extension

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/incidentsession"
	"github.com/danushkastanley/kube-memlens/internal/incidentsessionapi"
	"github.com/danushkastanley/kube-memlens/internal/memoryhistory"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apiserver/pkg/authentication/user"
	"k8s.io/apiserver/pkg/authorization/authorizer"
	apirequest "k8s.io/apiserver/pkg/endpoints/request"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

func incidentExtension(t *testing.T, options *IncidentSessionOptions) *Handler {
	t.Helper()
	reads, now := populatedReadHandler(t)
	h, err := NewHandler(testCoordinator(t, reads.store, now, 10), HandlerOptions{IncidentSessions: options,
		AgentUsername:    "system:serviceaccount:kube-memlens:kube-memlens-agent",
		MaxSnapshotBytes: 1 << 20, MaxConcurrent: 2, RequestsPerSec: 10, Burst: 2, MaxIdentities: 20})
	if err != nil {
		t.Fatal(err)
	}
	h.reads.podAuthorizer = authorizer.AuthorizerFunc(func(context.Context, authorizer.Attributes) (authorizer.Decision, string, error) {
		return authorizer.DecisionAllow, "", nil
	})
	return h
}

func incidentRequest(t *testing.T, method, path, body string) *http.Request {
	t.Helper()
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set(incidentsessionapi.SchemaHeader, "1")
	r.Header.Set("Content-Type", "application/json")
	factory := apirequest.RequestInfoFactory{APIPrefixes: sets.NewString("api", "apis"), GrouplessAPIPrefixes: sets.NewString("api")}
	info, err := factory.NewRequestInfo(r)
	if err != nil {
		t.Fatal(err)
	}
	ctx := apirequest.WithRequestInfo(r.Context(), info)
	return r.WithContext(apirequest.WithUser(ctx, &user.DefaultInfo{Name: "operator", Groups: []string{user.AllAuthenticated}}))
}

func TestIncidentSessionsDisabledByDefault(t *testing.T) {
	h := incidentExtension(t, nil)
	if err := h.configureIncidentSessions(context.Background(), "/does-not-exist"); err != nil {
		t.Fatal("disabled feature touched Kubernetes configuration")
	}
	if h.incidentSessions != nil {
		t.Fatal("disabled feature allocated storage")
	}
	for _, resource := range h.discoveryResources() {
		if strings.HasPrefix(resource.Name, incidentsessionapi.Resource) {
			t.Fatal("disabled feature advertised")
		}
	}
	mux := newTestRouteMux()
	h.Register(mux)
	w := &sessionRecorder{httptest.NewRecorder()}
	mux.ServeHTTP(w, incidentRequest(t, "POST", "/apis/"+readAPIVersion+"/namespaces/team-a/incidentsessions", "{}"))
	if w.Code >= 200 && w.Code < 300 {
		t.Fatal("disabled session created")
	}
	w = &sessionRecorder{httptest.NewRecorder()}
	mux.ServeHTTP(w, readRequest(t, "/apis/"+readAPIVersion+"/namespaces/team-a/pods", true))
	if w.Code != 200 {
		t.Fatal("existing reads changed")
	}
}

func TestIncidentSessionsConfigureRouteAndCancel(t *testing.T) {
	var namespaceReads atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/namespaces/team-a/pods/api" {
			fmt.Fprint(w, `{"apiVersion":"v1","kind":"Pod","metadata":{"name":"api","namespace":"team-a","uid":"uid-a"},"spec":{"nodeName":"node-a"}}`)
			return
		}
		if r.URL.Path != "/api/v1/namespaces/team-a" {
			t.Error("unexpected privileged namespace path")
		}
		namespaceReads.Add(1)
		fmt.Fprint(w, `{"apiVersion":"v1","kind":"Namespace","metadata":{"name":"team-a","uid":"namespace-uid"},"status":{"phase":"Active"}}`)
	}))
	defer server.Close()
	config := clientcmdapi.NewConfig()
	config.Clusters["test"] = &clientcmdapi.Cluster{Server: server.URL, CertificateAuthorityData: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})}
	config.AuthInfos["test"] = &clientcmdapi.AuthInfo{}
	config.Contexts["test"] = &clientcmdapi.Context{Cluster: "test", AuthInfo: "test"}
	config.CurrentContext = "test"
	path := filepath.Join(t.TempDir(), "config")
	if err := clientcmd.WriteToFile(*config, path); err != nil {
		t.Fatal(err)
	}
	h := incidentExtension(t, &IncidentSessionOptions{Namespaces: []string{"team-a"}})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := h.configureIncidentSessions(ctx, path); err != nil {
		t.Fatal(err)
	}
	defer h.incidentSessions.Shutdown()
	if len(h.incidentSessionResources()) != 9 {
		t.Fatal("missing session discovery")
	}
	for _, resource := range h.incidentSessionResources() {
		if !resource.Namespaced || sets.NewString(resource.Verbs...).HasAny("list", "watch", "update", "patch") {
			t.Fatal("unsupported session API advertised")
		}
	}
	mux := newTestRouteMux()
	h.Register(mux)
	collection := "/apis/" + readAPIVersion + "/namespaces/team-a/incidentsessions"
	w := &sessionRecorder{httptest.NewRecorder()}
	mux.ServeHTTP(w, incidentRequest(t, "POST", collection, "{}"))
	if w.Code != 201 {
		t.Fatalf("creation status=%d", w.Code)
	}
	var value incidentsessionapi.SessionResource
	if err := json.Unmarshal(w.Body.Bytes(), &value); err != nil {
		t.Fatal(err)
	}
	if namespaceReads.Load() != 2 {
		t.Fatal("namespace lifetime not checked at resolution and creation")
	}
	w = &sessionRecorder{httptest.NewRecorder()}
	mux.ServeHTTP(w, incidentRequest(t, "GET", collection+"/"+value.Metadata.Name, ""))
	if w.Code != 200 {
		t.Fatal("registered session route unavailable")
	}
	w = &sessionRecorder{httptest.NewRecorder()}
	mux.ServeHTTP(w, incidentRequest(t, "POST", collection+"/"+value.Metadata.Name+"/capture", `{"pod":"api"}`))
	if w.Code != 200 {
		t.Fatalf("capture status=%d", w.Code)
	}
	w = &sessionRecorder{httptest.NewRecorder()}
	mux.ServeHTTP(w, incidentRequest(t, "GET", collection+"/"+value.Metadata.Name+"/export-sensitive", ""))
	doc, err := incidentsession.DecodeExport(w.Body.Bytes())
	if w.Code != 200 || err != nil || doc.Authorised == nil || len(doc.Authorised.Captures) != 1 {
		t.Fatal("live acquired capture not retained", err)
	}
	if strings.Contains(string(doc.Authorised.Captures[0].Data), "uid-b") {
		t.Fatal("capture crossed tenant boundary")
	}
	digest := doc.Authorised.Captures[0].Digest
	w = &sessionRecorder{httptest.NewRecorder()}
	mux.ServeHTTP(w, incidentRequest(t, "POST", collection+"/"+value.Metadata.Name+"/compare", fmt.Sprintf(`{"before":%q,"after":%q}`, digest, digest)))
	if w.Code != 200 {
		t.Fatalf("comparison status=%d", w.Code)
	}
	w = &sessionRecorder{httptest.NewRecorder()}
	mux.ServeHTTP(w, incidentRequest(t, "GET", collection+"/"+value.Metadata.Name+"/export", ""))
	compared, err := incidentsession.DecodeExport(w.Body.Bytes())
	if err != nil || compared.Sanitised == nil || compared.Sanitised.Entries[len(compared.Sanitised.Entries)-1].Kind != incidentsession.Compared {
		t.Fatal("comparison missing from export", err)
	}
	historyReads, _ := contextReadFixture(t)
	h.reads.memoryHistory = historyReads.memoryHistory
	h.reads.now = historyReads.now
	query, err := memoryhistory.ParseQuery(url.Values{"source": {"prometheus"}}, memoryhistory.Pod, h.reads.now())
	if err != nil {
		t.Fatal(err)
	}
	markerBody, err := json.Marshal(map[string]any{"pod": "api", "query": query})
	if err != nil {
		t.Fatal(err)
	}
	w = &sessionRecorder{httptest.NewRecorder()}
	mux.ServeHTTP(w, incidentRequest(t, "POST", collection+"/"+value.Metadata.Name+"/markers", string(markerBody)))
	if w.Code != 200 {
		t.Fatalf("marker capture status=%d", w.Code)
	}
	w = &sessionRecorder{httptest.NewRecorder()}
	mux.ServeHTTP(w, incidentRequest(t, "GET", collection+"/"+value.Metadata.Name+"/export-sensitive", ""))
	marked, err := incidentsession.DecodeExport(w.Body.Bytes())
	if err != nil || marked.Authorised == nil || len(marked.Authorised.Captures) != 2 || marked.Authorised.Captures[1].SchemaVersion != 6 {
		t.Fatal("marker evidence missing from export", err)
	}
	cancel()
	w = &sessionRecorder{httptest.NewRecorder()}
	mux.ServeHTTP(w, incidentRequest(t, "GET", collection+"/"+value.Metadata.Name, ""))
	if w.Code != 503 {
		t.Fatal("cancelled collector served session state")
	}
}

// These route tests use an in-memory body; network deadlines are exercised in
// incidentsessionapi's real TLS transport tests.
type sessionRecorder struct{ *httptest.ResponseRecorder }

func (*sessionRecorder) SetReadDeadline(time.Time) error  { return nil }
func (*sessionRecorder) SetWriteDeadline(time.Time) error { return nil }
