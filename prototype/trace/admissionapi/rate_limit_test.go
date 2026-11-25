package admissionapi

import (
	"net/http/httptest"
	"strings"
	"testing"

	"k8s.io/apiserver/pkg/authentication/user"
	"k8s.io/apiserver/pkg/endpoints/request"
)

func requestAs(h *Handler, principal, method string) int {
	r := httptest.NewRequest(method, prefix+"/namespaces/tenant-a/traces/"+strings.Repeat("a", 32), nil)
	r = r.WithContext(request.WithUser(r.Context(), &user.DefaultInfo{Name: principal, Groups: []string{user.AllAuthenticated}}))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w.Code
}

// One caller in a loop must not stop another from cancelling a trace.
func TestOnePrincipalCannotExhaustAnotherPrincipalsRequests(t *testing.T) {
	h, _ := compatibilityHandler(t, 2)
	limited := false
	for range 200 {
		if requestAs(h, "noisy", "GET") == 429 {
			limited = true
		}
	}
	if !limited {
		t.Fatal("a caller sending 200 requests at once was never limited")
	}
	// 404 is the ownership boundary: the request passed the limiter.
	if code := requestAs(h, "operator", "DELETE"); code != 404 {
		t.Fatalf("another principal's cancellation got %d after one caller exhausted the limiter, want 404", code)
	}
}
