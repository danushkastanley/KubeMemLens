package nodebinding

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	admission "github.com/danushkastanley/kube-memlens/internal/traceadmission"
)

func TestLostResponseExpiresWithoutController(t *testing.T) {
	f := setup(t)
	// Discarding the response models a disconnected controller. The node must
	// release its retained directory without any subsequent get/delete request.
	if _, err := f.client.Bind(context.Background(), strings.Repeat("d", 32), workload(), time.Now().Add(250*time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	h := <-f.handles
	select {
	case <-h.closed:
	case <-time.After(2 * time.Second):
		t.Fatal("independent node expiry failed")
	}
}
func TestChangedCgroupInvalidatesLease(t *testing.T) {
	f := setup(t)
	b, err := f.client.Bind(context.Background(), strings.Repeat("e", 32), workload(), time.Now().Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	h := <-f.handles
	h.mu.Lock()
	h.failure = admission.ErrTargetChanged
	h.mu.Unlock()
	if err := b.Revalidate(context.Background()); !errors.Is(err, admission.ErrTargetChanged) {
		t.Fatalf("replacement: %v", err)
	}
	select {
	case <-h.closed:
	default:
		t.Fatal("invalid target handle retained")
	}
}
func TestServiceShutdownClosesRetainedHandles(t *testing.T) {
	f := setup(t)
	if _, err := f.client.Bind(context.Background(), strings.Repeat("f", 32), workload(), time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	h := <-f.handles
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := f.service.Close(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-h.closed:
	default:
		t.Fatal("shutdown retained handle")
	}
	if _, err := f.client.Bind(context.Background(), strings.Repeat("1", 32), workload(), time.Now().Add(time.Second)); !errors.Is(err, admission.ErrUnavailable) {
		t.Fatalf("admitted after shutdown: %v", err)
	}
}
func TestReplayStorageFailsClosed(t *testing.T) {
	f := setup(t)
	f.service.mu.Lock()
	for i := 0; i < 256; i++ {
		f.service.seen[string(rune(i))] = time.Now().Add(time.Second)
	}
	f.service.mu.Unlock()
	if _, err := f.client.Bind(context.Background(), strings.Repeat("2", 32), workload(), time.Now().Add(time.Second)); !errors.Is(err, admission.ErrCapacity) {
		t.Fatalf("full replay storage: %v", err)
	}
	if len(f.handles) != 0 {
		t.Fatal("work began without replay protection")
	}
}
