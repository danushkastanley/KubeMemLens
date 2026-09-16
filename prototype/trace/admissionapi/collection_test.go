package admissionapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	admission "github.com/danushkastanley/kube-memlens/internal/traceadmission"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apiserver/pkg/authentication/user"
	"k8s.io/apiserver/pkg/endpoints/request"
)

func TestUnsupportedCollectionsDoNotBlockNamespaceDeletion(t *testing.T) {
	manager, err := admission.NewManager(context.Background(), admission.Dependencies{Authorizer: denied{}, Resolver: noTarget{t}, Binder: noTarget{t}, Audit: func(admission.AuditEvent) {}}, admission.DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close(context.Background())
	for _, method := range []string{http.MethodGet, http.MethodDelete} {
		for _, query := range []string{"", "?limit=500", "?labelSelector=private"} {
			t.Run(method+query, func(t *testing.T) {
				r := httptest.NewRequest(method, prefix+"/namespaces/tenant-a/traces"+query, nil)
				r = r.WithContext(request.WithUser(r.Context(), &user.DefaultInfo{Name: "namespace-controller"}))
				w := httptest.NewRecorder()
				NewHandler(manager).ServeHTTP(w, r)
				var status metav1.Status
				if err := json.Unmarshal(w.Body.Bytes(), &status); err != nil {
					t.Fatal(err)
				}
				if w.Code != http.StatusMethodNotAllowed || !apierrors.IsMethodNotSupported(&apierrors.StatusError{ErrStatus: status}) {
					t.Fatalf("unsupported collection not recognised: %d %s", w.Code, w.Body.String())
				}
				if w.Header().Get("Allow") != http.MethodPost || w.Header().Get("Cache-Control") != "no-store" {
					t.Fatal("missing method/cache contract")
				}
			})
		}
	}
	w := httptest.NewRecorder()
	NewHandler(manager).ServeHTTP(w, httptest.NewRequest(http.MethodGet, prefix+"/namespaces/tenant-a/traces", nil))
	if w.Code != http.StatusUnauthorized {
		t.Fatal("unauthenticated collection probe accepted")
	}
	for _, resource := range resources() {
		for _, verb := range resource.Verbs {
			if verb == "list" || verb == "deletecollection" {
				t.Fatal("unsupported collection advertised")
			}
		}
	}
}
