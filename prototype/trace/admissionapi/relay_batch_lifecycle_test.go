package admissionapi

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/trace"
	admission "github.com/danushkastanley/kube-memlens/internal/traceadmission"
	"github.com/danushkastanley/kube-memlens/internal/traceframe"
	"github.com/danushkastanley/kube-memlens/prototype/trace/streamhttp"
	"k8s.io/apiserver/pkg/authentication/user"
)

type notifyingResponse struct {
	batchResponse
	flushed chan struct{}
}

func (w *notifyingResponse) FlushError() error { w.flushed <- struct{}{}; return nil }

func TestRelayFlushesSparseEventBeforeWaitingForNextFrame(t *testing.T) {
	lines := bytes.SplitAfter(relayBurst(t), []byte("\n"))
	input, source := io.Pipe()
	defer input.Close()
	defer source.Close()
	out := &notifyingResponse{batchResponse: batchResponse{ResponseRecorder: httptest.NewRecorder()}, flushed: make(chan struct{}, 2)}
	sink, err := streamhttp.NewSink(out)
	if err != nil {
		t.Fatal(err)
	}
	r := &relay{reader: traceframe.NewReader(input), sink: sink}
	done := make(chan error, 1)
	go func() {
		if _, err := r.reader.Next(); err != nil {
			done <- err
			return
		}
		first, err := r.reader.Next()
		if err != nil {
			done <- err
			return
		}
		_, err = r.forwardEvents(t.Context(), first)
		done <- err
	}()
	if _, err := source.Write(append(bytes.Clone(lines[0]), lines[1]...)); err != nil {
		t.Fatal(err)
	}
	select {
	case <-out.flushed:
	case <-time.After(time.Second):
		t.Fatal("sparse event waited for another frame")
	}
	if err := source.Close(); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err == nil || r.events != 1 || !bytes.Equal(out.Body.Bytes(), lines[1]) {
		t.Fatal("sparse delivery or incomplete-stream status changed")
	}
}

type revocableAuthorizer struct{ denied bool }

func (a *revocableAuthorizer) Trace(context.Context, user.Info, admission.Operation, string, string) error {
	if a.denied {
		return admission.ErrDenied
	}
	return nil
}
func (*revocableAuthorizer) Pod(context.Context, user.Info, string, string) error { return nil }

func TestRelayRevalidatesPermissionBeforeBufferedTerminal(t *testing.T) {
	f := &streamFixture{target: trace.TargetIdentity{Namespace: "tenant", PodName: "pod", PodUID: "uid", ContainerName: "worker", ContainerID: strings.Repeat("a", 64), ContainerStartedAt: time.Unix(100, 0).UTC(), NodeUID: "node", CgroupID: 42}}
	auth := &revocableAuthorizer{}
	manager, err := admission.NewManager(t.Context(), admission.Dependencies{Authorizer: auth, Resolver: f, Binder: f, AuditReferences: testAuditReferences(t), Audit: func(context.Context, admission.AuditEvent) error { return nil }}, admission.DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close(context.Background())
	intent, err := admission.DecodeRequest("tenant", strings.NewReader(`{"schemaVersion":1,"pod":"pod","container":"worker","kind":"files"}`))
	if err != nil {
		t.Fatal(err)
	}
	actor := &user.DefaultInfo{Name: "fixture", Groups: []string{user.AllAuthenticated}}
	accepted, err := manager.Admit(t.Context(), actor, intent)
	if err != nil {
		t.Fatal(err)
	}
	lease, err := manager.Claim(t.Context(), actor, "tenant", accepted.ID())
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close(context.Background())
	out := &batchResponse{ResponseRecorder: httptest.NewRecorder()}
	sink, err := streamhttp.NewSink(out)
	if err != nil {
		t.Fatal(err)
	}
	r := &relay{reader: traceframe.NewReader(bytes.NewReader(relayBurst(t))), sink: sink, lease: lease}
	first, err := r.reader.Next()
	if err != nil {
		t.Fatal(err)
	}
	auth.denied = true
	if err := r.run(t.Context(), first); !errors.Is(err, admission.ErrDenied) {
		t.Fatal("terminal skipped permission revalidation", err)
	}
	if r.events != 128 || bytes.Contains(out.Body.Bytes(), []byte(`"type":"summary"`)) {
		t.Fatal("unvalidated terminal was forwarded")
	}
}
