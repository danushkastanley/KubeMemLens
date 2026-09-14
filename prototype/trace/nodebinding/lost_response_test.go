package nodebinding

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	admission "github.com/danushkastanley/kube-memlens/internal/traceadmission"
)

func TestMalformedSuccessfulBindResponseRetainsCleanupHandle(t *testing.T) {
	f := setupHandler(t, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				next.ServeHTTP(w, r)
				return
			}
			response := httptest.NewRecorder()
			next.ServeHTTP(response, r)
			if response.Code != http.StatusOK {
				t.Error("fixture bind failed")
				w.WriteHeader(response.Code)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte("{"))
		})
	})
	binding, err := f.client.Bind(context.Background(), strings.Repeat("9", 32), workload(), testIntent(), time.Now().Add(time.Second))
	if !errors.Is(err, admission.ErrUnavailable) || binding == nil {
		t.Fatal("uncertain bind discarded cleanup authority")
	}
	handle := <-f.handles
	if err := binding.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-handle.closed:
	default:
		t.Fatal("lost response leaked node handle")
	}
}
func TestRejectedReplayDoesNotReturnAuthorityToCloseOriginal(t *testing.T) {
	f := setup(t)
	id := strings.Repeat("8", 32)
	expires := time.Now().Add(time.Second)
	original, err := f.client.Bind(context.Background(), id, workload(), testIntent(), expires)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := f.client.Bind(context.Background(), id, workload(), testIntent(), expires)
	if !errors.Is(err, admission.ErrExpired) || replay != nil {
		t.Fatal("replay obtained original cleanup handle")
	}
	if err := original.Revalidate(context.Background()); err != nil {
		t.Fatal("replay invalidated original")
	}
	if err := original.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}
