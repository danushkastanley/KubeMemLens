package traceclient

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/trace"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestCancellationHasReservedControlCapacity(t *testing.T) {
	entered := make(chan struct{})
	c, _ := fixtureClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			close(entered)
			<-r.Context().Done()
			return
		}
		_ = json.NewEncoder(w).Encode(metav1.Status{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Status"}, Status: metav1.StatusSuccess, Code: 200})
	}))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := c.Inspect(ctx, "tenant-a", strings.Repeat("c", 32)); done <- err }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("control read did not start")
	}
	plan := planFixture(t, c, preflightFixture(t, selectionFixture(), DefaultIntent(trace.Files)))
	_, err := c.Create(context.Background(), plan)
	var local *Error
	if !errors.As(err, &local) || local.Kind != Capacity {
		t.Fatal("local capacity became ambiguous mutation", err)
	}
	cleanup, err := c.CancelID(context.Background(), "tenant-a", strings.Repeat("c", 32))
	if err != nil || cleanup != CleanupConfirmed {
		t.Fatal("control read blocked cancellation", err)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("read did not cancel")
	}
}

func TestEveryActivationStreamUsesFreshConnection(t *testing.T) {
	metadata, _, summary := streamFixture(t, DefaultIntent(trace.Files), 0)
	var mu sync.Mutex
	addresses := map[string]bool{}
	c, _ := fixtureClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		addresses[r.RemoteAddr] = true
		mu.Unlock()
		if r.ProtoMajor != 1 || !r.Close {
			t.Error("stream connection could be reused")
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		_, _ = w.Write(metadata)
		_, _ = w.Write(summary)
	}))
	for range 2 {
		if _, err := c.Watch(context.Background(), watchAdmission(t, c, DefaultIntent(trace.Files)), nil); err != nil {
			t.Fatal(err)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(addresses) != 2 {
		t.Fatal("explicit stream requests reused one connection")
	}
}
