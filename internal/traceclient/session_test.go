package traceclient

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/trace"
	"github.com/danushkastanley/kube-memlens/internal/traceframe"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type sessionServer struct {
	active                    chan struct{}
	stop                      chan struct{}
	streamDone                chan struct{}
	once                      sync.Once
	creates, streams, deletes atomic.Int64
}

func sessionFixture(t *testing.T, mode string) (*Session, *sessionServer) {
	t.Helper()
	server := &sessionServer{active: make(chan struct{}), stop: make(chan struct{}), streamDone: make(chan struct{})}
	intent := DefaultIntent(trace.Files)
	doc := preflightFixture(t, selectionFixture(), intent)
	metadata, _, terminal := streamFixture(t, intent, 0)
	if mode == "cancel" || mode == "denied-cancel" {
		frame, err := traceframe.Decode(terminal)
		if err != nil {
			t.Fatal(err)
		}
		summary, err := frame.ClientSummary()
		if err != nil {
			t.Fatal(err)
		}
		summary.Termination = trace.Cancelled
		summary.SessionEndedAt = summary.ObservationStartedAt.Add(time.Millisecond)
		ended := summary.SessionEndedAt
		summary.ObservationEndedAt = &ended
		frame, err = traceframe.NewSummaryVersion(summary, 2)
		if err != nil {
			t.Fatal(err)
		}
		terminal, err = traceframe.Encode(frame)
		if err != nil {
			t.Fatal(err)
		}
	}
	c, _ := fixtureClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == apiPrefix:
			discoverFixture(w)
		case strings.HasSuffix(r.URL.Path, "/tracepreflights"):
			_ = json.NewEncoder(w).Encode(doc)
		case r.Method == "POST":
			server.creates.Add(1)
			if mode == "lost-create" {
				panic(http.ErrAbortHandler)
			}
			w.WriteHeader(201)
			_ = json.NewEncoder(w).Encode(admittedFixture("tenant-a", strings.Repeat("c", 32)))
		case strings.HasSuffix(r.URL.Path, "/stream"):
			server.streams.Add(1)
			defer close(server.streamDone)
			w.Header().Set("Content-Type", "application/x-ndjson")
			_, _ = w.Write(metadata)
			w.(http.Flusher).Flush()
			close(server.active)
			if mode == "cancel" || mode == "denied-cancel" {
				select {
				case <-server.stop:
				case <-r.Context().Done():
					return
				}
			}
			if mode != "missing-summary" {
				_, _ = w.Write(terminal)
			}
		case r.Method == "DELETE":
			server.deletes.Add(1)
			if mode == "denied-cancel" {
				http.Error(w, "denied", 403)
				return
			}
			server.once.Do(func() { close(server.stop) })
			select {
			case <-server.streamDone:
			case <-r.Context().Done():
				return
			}
			_ = json.NewEncoder(w).Encode(metav1.Status{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Status"}, Status: metav1.StatusSuccess, Code: 200})
		default:
			http.NotFound(w, r)
		}
	}))
	s, err := NewSession(c, selectionFixture(), intent)
	if err != nil {
		t.Fatal(err)
	}
	return s, server
}
func waitSession(t *testing.T, s *Session) Snapshot {
	t.Helper()
	select {
	case <-s.Done():
		return s.Snapshot()
	case <-time.After(3 * time.Second):
		s.Cancel()
		t.Fatal("session did not terminate")
	}
	return Snapshot{}
}
func prepareSession(t *testing.T, s *Session) {
	t.Helper()
	if err := s.Prepare(context.Background()); err != nil {
		t.Fatal(err)
	}
	if s.Snapshot().State != StateReady {
		t.Fatal("preflight did not become ready")
	}
}
func TestSessionRequiresReviewBeforeOneAdmissionAndRetainsResult(t *testing.T) {
	s, server := sessionFixture(t, "complete")
	if err := s.Start(context.Background()); err == nil {
		t.Fatal("unprepared session started")
	}
	prepareSession(t, s)
	if server.creates.Load() != 0 {
		t.Fatal("preflight created a session")
	}
	if err := s.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	result := waitSession(t, s)
	if result.State != StateCompleted || result.Cleanup != CleanupConfirmed || !result.Result.TransportComplete || result.Result.StreamVersion != 2 {
		t.Fatal("completion state incomplete", result.State, result.Failure)
	}
	if err := s.Start(context.Background()); err == nil {
		t.Fatal("terminal session restarted")
	}
	s.Cancel()
	if server.creates.Load() != 1 || server.streams.Load() != 1 || server.deletes.Load() != 1 {
		t.Fatal("session replayed an operation")
	}
}
func TestSessionCancelKeepsReaderUntilServerConfirmation(t *testing.T) {
	for _, mode := range []string{"cancel", "denied-cancel"} {
		t.Run(mode, func(t *testing.T) {
			s, server := sessionFixture(t, mode)
			prepareSession(t, s)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if err := s.Start(ctx); err != nil {
				t.Fatal(err)
			}
			select {
			case <-server.active:
			case <-time.After(time.Second):
				t.Fatal("stream inactive")
			}
			cancel()
			s.Cancel()
			s.Cancel()
			result := waitSession(t, s)
			if server.deletes.Load() != 1 {
				t.Fatal("cancellation replayed")
			}
			if mode == "cancel" {
				if result.State != StateCancelled || result.Cleanup != CleanupConfirmed || !result.Result.TransportComplete {
					t.Fatal("reader closed before cancellation receipt", result.State, result.Failure)
				}
			} else if result.State != StateFailed || result.Cleanup != CleanupUnconfirmed {
				t.Fatal("denied cancel became success")
			}
		})
	}
}
func TestSessionUnknownCreateAndPartialStreamRemainFailures(t *testing.T) {
	for _, mode := range []string{"lost-create", "missing-summary"} {
		t.Run(mode, func(t *testing.T) {
			s, server := sessionFixture(t, mode)
			prepareSession(t, s)
			if err := s.Start(context.Background()); err != nil {
				t.Fatal(err)
			}
			result := waitSession(t, s)
			if result.State != StateFailed || result.Failure == nil || result.Result.TransportComplete {
				t.Fatal("incomplete workflow became success")
			}
			if mode == "lost-create" && (server.creates.Load() != 1 || server.streams.Load() != 0 || server.deletes.Load() != 0 || result.Cleanup != CleanupUnconfirmed) {
				t.Fatal("unknown creation was replayed or guessed")
			}
			var typed *Error
			if errors.As(result.Failure, &typed) {
				typed.Kind = Invalid
				if s.Snapshot().Failure.(*Error).Kind == Invalid {
					t.Fatal("snapshot shared mutable error")
				}
			}
		})
	}
}
func TestSessionCancelBeforeStartCreatesNothing(t *testing.T) {
	for _, prepare := range []bool{false, true} {
		s, server := sessionFixture(t, "complete")
		if prepare {
			prepareSession(t, s)
		}
		s.Cancel()
		result := waitSession(t, s)
		if result.State != StateCancelled || result.Cleanup != CleanupNotRequested || server.creates.Load() != 0 {
			t.Fatal("pre-start cancellation created work")
		}
	}
}
