package incidentsessionapi

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/synctest"

	"github.com/danushkastanley/kube-memlens/internal/incidentsession"
	"k8s.io/apiserver/pkg/authorization/authorizer"
)

func TestSessionRequestValidation(t *testing.T) {
	for _, test := range []struct {
		name, method, suffix, body string
		code                       int
	}{
		{"null", "POST", "", "null", 400},
		{"unknown owner", "POST", "", `{"actor":"alice"}`, 400},
		{"forged namespace", "POST", "", `{"namespaceUID":"team-a-uid"}`, 400},
		{"duplicate", "POST", "/entries", `{"note":"one","note":"two"}`, 400},
		{"case alias", "POST", "/entries", `{"Note":"one"}`, 400},
		{"forged receipt", "POST", "/entries", `{"note":"one","references":[]}`, 400},
		{"invalid unicode", "POST", "/entries", `{"note":"\ud800"}`, 400},
		{"multiline", "POST", "/entries", `{"note":"one\ntwo"}`, 400},
		{"oversize note", "POST", "/entries", `{"note":"` + strings.Repeat("a", 513) + `"}`, 400},
		{"oversize body", "POST", "/entries", strings.Repeat(" ", MaxRequestBytes+1), 400},
		{"extra object", "POST", "", "{} {}", 400},
		{"query", "GET", "/export?watch=true", "", 400},
		{"extra path", "GET", "/export/extra", "", 400},
		{"unsupported action", "POST", "/unknown", "{}", 404},
		{"put", "PUT", "/entries", `{"note":"one"}`, 404},
		{"get body", "GET", "/export", "{}", 400},
	} {
		t.Run(test.name, func(t *testing.T) {
			h := testHandler(t)
			id := createdID(t, request(t, h, caller("alice"), "POST", collectionPath, "{}"))
			path := collectionPath
			if test.suffix != "" {
				path += "/" + id + test.suffix
			}
			if w := request(t, h, caller("alice"), test.method, path, test.body); w.Code != test.code {
				t.Fatalf("status=%d want=%d", w.Code, test.code)
			}
			w := request(t, h, caller("alice"), "GET", collectionPath+"/"+id+"/export", "")
			decoded, err := incidentsession.DecodeExport(w.Body.Bytes())
			if err != nil || decoded.Sanitised == nil || len(decoded.Sanitised.Entries) != 1 {
				t.Fatal("invalid request mutated session")
			}
		})
	}
}

func TestSessionSchemaMediaAndAuthentication(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*http.Request)
		code   int
	}{
		{"missing schema", func(r *http.Request) { r.Header.Del(SchemaHeader) }, 400},
		{"future schema", func(r *http.Request) { r.Header.Set(SchemaHeader, "2") }, 400},
		{"duplicate schema", func(r *http.Request) { r.Header.Add(SchemaHeader, "1") }, 400},
		{"wrong media", func(r *http.Request) { r.Header.Set("Content-Type", "text/plain") }, 400},
		{"wrong charset", func(r *http.Request) { r.Header.Set("Content-Type", "application/json; charset=utf-16") }, 400},
		{"compressed", func(r *http.Request) { r.Header.Set("Content-Encoding", "gzip") }, 400},
	} {
		t.Run(test.name, func(t *testing.T) {
			h := testHandler(t)
			r := httptest.NewRequest("POST", collectionPath, strings.NewReader("{}"))
			r.Header.Set(SchemaHeader, "1")
			r.Header.Set("Content-Type", "application/json")
			test.change(r)
			w := newBufferedRecorder()
			h.ServeHTTP(w, withRequestContext(t, r, caller("alice")))
			if w.Code != test.code {
				t.Fatalf("status=%d", w.Code)
			}
		})
	}
	h := testHandler(t)
	if w := request(t, h, nil, "POST", collectionPath, "{}"); w.Code != 401 {
		t.Fatal("unauthenticated request accepted")
	}
	w := newBufferedRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", collectionPath, strings.NewReader("{}")))
	if w.Code != 503 {
		t.Fatal("missing trusted request context accepted")
	}
}

func TestSessionReauthorisesBeforeMutation(t *testing.T) {
	h := testHandler(t)
	calls := 0
	h.authority.delegate = authorizer.AuthorizerFunc(func(context.Context, authorizer.Attributes) (authorizer.Decision, string, error) {
		calls++
		if calls == 1 {
			return authorizer.DecisionAllow, "", nil
		}
		return authorizer.DecisionDeny, "", nil
	})
	if w := request(t, h, caller("alice"), "POST", collectionPath, "{}"); w.Code != 403 || calls != 2 {
		t.Fatal("revocation between resolution and mutation ignored")
	}
}

func TestSessionAdmissionBounds(t *testing.T) {
	t.Run("namespace rate", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			h := testHandler(t)
			for i := 0; i < 8; i++ {
				if w := request(t, h, caller("alice"), "GET", collectionPath+"/"+strings.Repeat("a", 32), ""); w.Code != 404 {
					t.Fatalf("request %d status=%d", i, w.Code)
				}
			}
			if w := request(t, h, caller("alice"), "GET", collectionPath+"/"+strings.Repeat("a", 32), ""); w.Code != 429 {
				t.Fatal("namespace rate bound absent")
			}
		})
	})
	t.Run("in flight", func(t *testing.T) {
		h := testHandler(t)
		h.gate <- struct{}{}
		h.gate <- struct{}{}
		if w := request(t, h, caller("alice"), "POST", collectionPath, "{}"); w.Code != 429 {
			t.Fatal("in-flight bound absent")
		}
	})
}

type failingBody struct{}

func (failingBody) Read([]byte) (int, error) { return 0, errors.New("private read failure") }
func (failingBody) Close() error             { return nil }

func TestSessionBodyReadFailure(t *testing.T) {
	r := httptest.NewRequest("POST", collectionPath, nil)
	r.Header.Set("Content-Type", "application/json")
	r.Body = failingBody{}
	if readBody(r, &struct{}{}) == nil || emptyBody(r) {
		t.Fatal("read failure treated as valid body")
	}
}

type shortWriter struct {
	header  http.Header
	failure error
}

func (w *shortWriter) Header() http.Header         { return w.header }
func (*shortWriter) WriteHeader(int)               {}
func (w *shortWriter) Write(p []byte) (int, error) { return len(p) - 1, w.failure }

func TestSessionAbortsPartialExport(t *testing.T) {
	for _, err := range []error{nil, io.ErrClosedPipe} {
		func() {
			defer func() {
				if recover() != http.ErrAbortHandler {
					t.Error("partial response was not aborted")
				}
			}()
			writeBytes(&shortWriter{header: http.Header{}, failure: err}, 200, []byte(`{"private":"value"}`))
		}()
	}
}
