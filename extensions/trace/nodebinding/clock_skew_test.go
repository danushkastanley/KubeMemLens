package nodebinding

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/trace"
	admission "github.com/danushkastanley/kube-memlens/internal/traceadmission"
	"github.com/danushkastanley/kube-memlens/internal/traceframe"
)

// These tests run on one clock. A node whose clock is behind the controller's
// sees controller instants that much further in its future, so the lag is
// modelled by adding it to the instants the controller sends.

type auditLog struct {
	mu     sync.Mutex
	events []string
}

func (l *auditLog) record(event string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.events = append(l.events, event)
}
func (l *auditLog) count(event string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := 0
	for _, e := range l.events {
		if e == event {
			n++
		}
	}
	return n
}

func oneSecondIntent(t *testing.T) admission.Request {
	t.Helper()
	intent, err := admission.DecodeRequest("tenant-a", strings.NewReader(`{"schemaVersion":1,"pod":"target","container":"worker","kind":"files","durationSeconds":1}`))
	if err != nil {
		t.Fatal(err)
	}
	return intent
}

func bindSeenBy(f *fixture, id string, intent admission.Request, expires time.Time) error {
	r := requestFor(id, workload(), intent, expires)
	r.controller = strings.Repeat("c", 32)
	_, err := f.service.bind(context.Background(), r)
	return err
}

func TestNodeBehindControllerWithinAllowanceBindsAndStreams(t *testing.T) {
	const lag = 500 * time.Millisecond
	f := setupRuntime(t, memoryRuntime{})
	intent := oneSecondIntent(t)

	// The controller grants the full pending lifetime.
	bound := time.Now()
	pending := strings.Repeat("1", 32)
	requested := bound.Add(maxLease + lag)
	if err := bindSeenBy(f, pending, intent, requested); err != nil {
		t.Fatalf("bind refused for a node %v behind: %v", lag, err)
	}
	f.service.mu.Lock()
	held := f.service.leases[pending].expires.Sub(bound)
	replayRefusedUntil := f.service.seen[pending]
	f.service.mu.Unlock()
	if held > maxLease+lag/2 {
		t.Fatalf("pending binding held for %v, longer than %v", held, maxLease)
	}
	// The request stays acceptable until the instant the controller sent, so
	// its replay record must last that long too.
	if replayRefusedUntil.Before(requested) {
		t.Fatalf("replay record ends %v before the request does", requested.Sub(replayRefusedUntil))
	}

	// The controller grants the full admitted duration.
	id := strings.Repeat("2", 32)
	binding, err := f.client.Bind(context.Background(), id, workload(), intent, time.Now().Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	leaseDeadline := time.Now().Add(intent.Bounds().Duration + lag)
	source, err := binding.(StreamBinding).OpenStream(context.Background(), leaseDeadline, StreamIdentity{traceframe.Version, tracepreflightDigest(), fixtureProgramme})
	if err != nil {
		t.Fatalf("stream refused for a node %v behind: %v", lag, err)
	}
	defer source.Close()
	reader := traceframe.NewReader(source)
	first, err := reader.Next()
	if err != nil {
		t.Fatal(err)
	}
	spec, err := trace.NewSpecification(intent.Kind(), binding.Target(), intent.Paths(), intent.Bounds())
	if err != nil {
		t.Fatal(err)
	}
	if err := first.MatchAdmission(id, tracepreflightDigest(), fixtureProgramme, spec, leaseDeadline); err != nil {
		t.Fatalf("controller refused the node's shortened deadline: %v", err)
	}
	// The node runs to its own clock plus the admitted duration, not to the
	// controller's later instant.
	view, err := first.ClientMetadata()
	if err != nil {
		t.Fatal(err)
	}
	if !view.Deadline.Before(leaseDeadline) || view.Deadline.Sub(view.SessionStartedAt) > intent.Bounds().Duration {
		t.Fatalf("node deadline %v after its start, lease deadline %v later: not shortened to the admitted %v", view.Deadline.Sub(view.SessionStartedAt), leaseDeadline.Sub(view.Deadline), intent.Bounds().Duration)
	}
	for {
		_, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("stream did not reach its terminal summary: %v", err)
		}
	}
	if _, events := reader.Counts(); events != 1 {
		t.Fatal("stream lost its event")
	}
}

func TestNodeBehindControllerBeyondAllowanceIsUnavailable(t *testing.T) {
	const lag = 10 * time.Second
	f := setupRuntime(t, memoryRuntime{})
	audit := &auditLog{}
	f.service.audit = audit.record
	intent := oneSecondIntent(t)

	if err := bindSeenBy(f, strings.Repeat("1", 32), intent, time.Now().Add(maxLease+lag)); !errors.Is(err, admission.ErrUnavailable) {
		t.Fatalf("bind for a node %v behind: got %v, want unavailable", lag, err)
	}
	if audit.count("clock_skew") != 1 {
		t.Fatal("refused bind did not record clock_skew")
	}

	id := strings.Repeat("2", 32)
	binding, err := f.client.Bind(context.Background(), id, workload(), intent, time.Now().Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	source, err := binding.(StreamBinding).OpenStream(context.Background(), time.Now().Add(intent.Bounds().Duration+lag), StreamIdentity{traceframe.Version, tracepreflightDigest(), fixtureProgramme})
	if source != nil {
		source.Close()
	}
	if !errors.Is(err, admission.ErrUnavailable) {
		t.Fatalf("stream for a node %v behind: got %v, want unavailable", lag, err)
	}
	if audit.count("clock_skew") != 2 {
		t.Fatal("refused stream did not record clock_skew")
	}
}

func TestBindForAnInstantAlreadyPastIsExpired(t *testing.T) {
	f := setup(t)
	if err := bindSeenBy(f, strings.Repeat("1", 32), oneSecondIntent(t), time.Now().Add(-time.Millisecond)); !errors.Is(err, admission.ErrExpired) {
		t.Fatalf("got %v, want expired", err)
	}
}
