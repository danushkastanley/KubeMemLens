package incidentsessionapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/incidentsession"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apiserver/pkg/authentication/user"
	"k8s.io/apiserver/pkg/authorization/authorizer"
	apirequest "k8s.io/apiserver/pkg/endpoints/request"
)

const collectionPath = "/apis/" + api.MemoryAPIGroup + "/" + api.MemoryAPIVersion + "/namespaces/team-a/" + Resource

func testHandler(t *testing.T) *Handler {
	t.Helper()
	a, err := NewAuthority([]string{"team-a", "team-b"}, namespaceFunc(func(_ context.Context, name string) (string, error) { return name + "-uid", nil }), authorizer.AuthorizerFunc(func(context.Context, authorizer.Attributes) (authorizer.Decision, string, error) {
		return authorizer.DecisionAllow, "", nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	h, err := NewHandler(context.Background(), a, incidentsession.DefaultLimits(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.Shutdown)
	return h
}

// Inject a trusted identity in component tests only. Actual Kubernetes
// authentication and SubjectAccessReview require the separate cluster test.
func withRequestContext(t *testing.T, r *http.Request, identity user.Info) *http.Request {
	t.Helper()
	factory := apirequest.RequestInfoFactory{APIPrefixes: sets.NewString("api", "apis"), GrouplessAPIPrefixes: sets.NewString("api")}
	info, err := factory.NewRequestInfo(r)
	if err != nil {
		t.Fatal(err)
	}
	ctx := apirequest.WithRequestInfo(r.Context(), info)
	if identity != nil {
		ctx = apirequest.WithUser(ctx, identity)
	}
	return r.WithContext(ctx)
}

// Buffered component requests have no network IO. Real transport deadlines are
// exercised by the TLS workflow and the slow-body regression.
type bufferedRecorder struct{ *httptest.ResponseRecorder }

func (*bufferedRecorder) SetReadDeadline(time.Time) error  { return nil }
func (*bufferedRecorder) SetWriteDeadline(time.Time) error { return nil }
func newBufferedRecorder() *bufferedRecorder               { return &bufferedRecorder{httptest.NewRecorder()} }

func request(t *testing.T, h *Handler, identity user.Info, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set(SchemaHeader, "1")
	r.Header.Set("Content-Type", "application/json")
	w := newBufferedRecorder()
	h.ServeHTTP(w, withRequestContext(t, r, identity))
	return w.ResponseRecorder
}

func createdID(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	if w.Code != http.StatusCreated {
		t.Fatalf("create status %d", w.Code)
	}
	var value SessionResource
	if err := json.Unmarshal(w.Body.Bytes(), &value); err != nil {
		t.Fatal(err)
	}
	if value.Kind != "IncidentSession" || value.SchemaVersion != 1 || value.Metadata.Namespace != "team-a" || value.Session.Entries != 1 || value.Session.ID != value.Metadata.Name {
		t.Fatal("incorrect session resource")
	}
	return value.Metadata.Name
}

func TestSessionHTTPWorkflow(t *testing.T) {
	h := testHandler(t)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.ServeHTTP(w, withRequestContext(t, r, caller("alice")))
	}))
	defer server.Close()
	call := func(method, path, body string, status int) []byte {
		t.Helper()
		r, err := http.NewRequest(method, server.URL+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		r.Header.Set(SchemaHeader, "1")
		r.Header.Set("Content-Type", "application/json")
		response, err := server.Client().Do(r)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		data, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != status {
			t.Fatalf("%s %s status=%d", method, path, response.StatusCode)
		}
		if response.Header.Get("Cache-Control") != "no-store" || response.Header.Get(SchemaHeader) != "1" {
			t.Fatal("privacy/schema headers missing")
		}
		return data
	}
	var started SessionResource
	if err := json.Unmarshal(call("POST", collectionPath, "{}", 201), &started); err != nil {
		t.Fatal(err)
	}
	path := collectionPath + "/" + started.Metadata.Name
	call("POST", path+"/entries", `{"note":"private annotation /secret/path"}`, 200)
	sanitised := call("GET", path+"/export", "", 200)
	decoded, err := incidentsession.DecodeExport(sanitised)
	if err != nil || decoded.Sanitised == nil {
		t.Fatal("invalid sanitised export", err)
	}
	for _, private := range []string{"team-a", "alice", "private annotation", "/secret/path", started.Metadata.Name} {
		if strings.Contains(string(sanitised), private) {
			t.Fatal("sanitised export leaked private field")
		}
	}
	full := call("GET", path+"/export-sensitive", "", 200)
	decoded, err = incidentsession.DecodeExport(full)
	if err != nil || decoded.Authorised == nil || !strings.Contains(string(full), "private annotation /secret/path") {
		t.Fatal("authorised export missing evidence", err)
	}
	call("GET", path, "", 200)
	var closed SessionResource
	if err := json.Unmarshal(call("POST", path+"/close", "{}", 200), &closed); err != nil {
		t.Fatal(err)
	}
	if closed.Session.ClosedAt == nil || closed.Session.Entries != 3 {
		t.Fatal("close not recorded")
	}
	call("DELETE", path, "", 204)
	call("GET", path, "", 404)
}

func TestSessionOwnerAndNamespaceIsolation(t *testing.T) {
	for _, suffix := range []string{"", "/export", "/export-sensitive"} {
		t.Run(suffix, func(t *testing.T) {
			h := testHandler(t)
			id := createdID(t, request(t, h, caller("alice"), "POST", collectionPath, "{}"))
			path := collectionPath + "/" + id + suffix
			if w := request(t, h, caller("bob"), "GET", path, ""); w.Code != 404 {
				t.Fatalf("other actor status=%d", w.Code)
			}
			if w := request(t, h, caller("alice"), "GET", strings.Replace(path, "team-a", "team-b", 1), ""); w.Code != 404 {
				t.Fatalf("other namespace status=%d", w.Code)
			}
			h.authority.resolver = namespaceFunc(func(context.Context, string) (string, error) { return "recreated-uid", nil })
			if w := request(t, h, caller("alice"), "GET", path, ""); w.Code != 404 {
				t.Fatalf("recreated namespace status=%d", w.Code)
			}
		})
	}
}

func TestSessionRevocationAndSensitiveExportPermission(t *testing.T) {
	h := testHandler(t)
	id := createdID(t, request(t, h, caller("alice"), "POST", collectionPath, "{}"))
	path := collectionPath + "/" + id
	h.authority.delegate = authorizer.AuthorizerFunc(func(_ context.Context, a authorizer.Attributes) (authorizer.Decision, string, error) {
		if a.GetSubresource() == "export" {
			return authorizer.DecisionAllow, "", nil
		}
		return authorizer.DecisionDeny, "private reason", nil
	})
	if w := request(t, h, caller("alice"), "GET", path+"/export", ""); w.Code != 200 {
		t.Fatal("sanitised permission not independent")
	}
	for _, suffix := range []string{"", "/export-sensitive"} {
		if w := request(t, h, caller("alice"), "GET", path+suffix, ""); w.Code != 403 || strings.Contains(w.Body.String(), "private reason") {
			t.Fatal("revoked permission ignored or reason leaked")
		}
	}
}

func TestSessionClosedAndShutdown(t *testing.T) {
	h := testHandler(t)
	id := createdID(t, request(t, h, caller("alice"), "POST", collectionPath, "{}"))
	path := collectionPath + "/" + id
	if w := request(t, h, caller("alice"), "POST", path+"/close", "{}"); w.Code != 200 {
		t.Fatal("close failed")
	}
	if w := request(t, h, caller("alice"), "POST", path+"/entries", `{"note":"late"}`); w.Code != 409 {
		t.Fatal("closed session mutated")
	}
	h.Shutdown()
	if w := request(t, h, caller("alice"), "GET", path, ""); w.Code != 503 {
		t.Fatal("shutdown served state")
	}
}
