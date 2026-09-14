package nodebinding

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	admission "github.com/danushkastanley/kube-memlens/internal/traceadmission"
)

func TestReplacementNodeCannotConfirmPredecessorCleanup(t *testing.T) {
	var current atomic.Value
	f := setupHandler(t, func(h http.Handler) http.Handler {
		current.Store(h)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { current.Load().(http.Handler).ServeHTTP(w, r) })
	})
	original, err := f.client.Bind(context.Background(), strings.Repeat("5", 32), workload(), testIntent(), time.Now().Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	oldHandle := <-f.handles
	replacement, err := NewService(context.Background(), f.service.nodeUID, f.service.nodeName, f.service.control, f.service.resolve, f.service.preflight, func(string) {}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer replacement.Close(context.Background())
	if replacement.instance == f.service.instance {
		t.Fatal("node process identity was reused")
	}
	current.Store(http.Handler(replacement))
	if err := original.Close(context.Background()); !errors.Is(err, admission.ErrUnavailable) {
		t.Fatal("replacement confirmed unknown old resources absent")
	}
	select {
	case <-oldHandle.closed:
		t.Fatal("replacement closed predecessor reference")
	default:
	}
	if err := f.service.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}
func TestAnotherControllerInstanceCannotAdoptBinding(t *testing.T) {
	f := setup(t)
	id := strings.Repeat("4", 32)
	original, err := f.client.Bind(context.Background(), id, workload(), testIntent(), time.Now().Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	other, err := NewClient(f.control, []Endpoint{{NodeUID: "node-uid", NodeName: "node", URL: f.server.URL, Peer: f.node}})
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	client := other.nodes["node-uid"]
	instance, err := client.identity(context.Background(), "node-uid")
	if err != nil {
		t.Fatal(err)
	}
	if response, err := client.call(context.Background(), http.MethodDelete, "/v1/bindings/"+id, nil, instance); err == nil {
		response.Body.Close()
		t.Fatal("new controller adopted cleanup authority")
	}
	if err := original.Revalidate(context.Background()); err != nil {
		t.Fatal("other controller invalidated binding")
	}
	if err := original.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}
