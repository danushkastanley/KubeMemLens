package nodebinding

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	admission "github.com/danushkastanley/kube-memlens/internal/traceadmission"
)

// A bind request stays acceptable until its own expiry. A trace that ends
// sooner must not make the node forget that it has already used the request.
func TestBindRequestCannotBeReplayedAfterShortTraceEnds(t *testing.T) {
	f := setup(t)
	id := strings.Repeat("9", 32)
	expires := time.Now().Add(10 * time.Second)
	if _, err := f.client.Bind(context.Background(), id, workload(), testIntent(), expires); err != nil {
		t.Fatal(err)
	}
	execution, err := f.service.activate(context.Background(), id, time.Now().Add(100*time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	<-execution.ctx.Done()
	if err := execution.finish(); err != nil {
		t.Fatal(err)
	}
	replayed, err := f.client.Bind(context.Background(), id, workload(), testIntent(), expires)
	if !errors.Is(err, admission.ErrExpired) {
		t.Fatalf("bind request replayed %v before its expiry, after its trace ended: got %v, want expired", time.Until(expires).Round(time.Second), err)
	}
	if replayed != nil {
		t.Fatal("replayed bind request returned a binding")
	}
}
