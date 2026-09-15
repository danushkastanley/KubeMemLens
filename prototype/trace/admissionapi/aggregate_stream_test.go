package admissionapi

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/trace"
	admission "github.com/danushkastanley/kube-memlens/internal/traceadmission"
	"github.com/danushkastanley/kube-memlens/internal/traceaggregate"
	"github.com/danushkastanley/kube-memlens/internal/traceframe"
	"github.com/danushkastanley/kube-memlens/internal/tracepreflight"
	"github.com/danushkastanley/kube-memlens/prototype/trace/nodebinding"
	"k8s.io/apiserver/pkg/authentication/user"
)

const streamDigest = "sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"

type streamFixture struct {
	target    trace.TargetIdentity
	id        string
	spec      trace.Specification
	version   int
	truncated bool
}

func (*streamFixture) Trace(context.Context, user.Info, admission.Operation, string, string) error {
	return nil
}
func (*streamFixture) Pod(context.Context, user.Info, string, string) error { return nil }
func (f *streamFixture) Resolve(context.Context, admission.Request) (admission.Workload, error) {
	target := f.target
	target.CgroupID = 0
	return admission.Workload{Target: target, NodeName: "node", QoS: "Burstable"}, nil
}
func (*streamFixture) Revalidate(context.Context, admission.Workload) error { return nil }
func (f *streamFixture) Bind(_ context.Context, id string, _ admission.Workload, r admission.Request, _ time.Time) (admission.Binding, error) {
	f.id = id
	var err error
	f.spec, err = trace.NewSpecification(r.Kind(), f.target, r.Paths(), r.Bounds())
	return &fixtureBinding{f}, err
}

type fixtureBinding struct{ fixture *streamFixture }

func (b *fixtureBinding) Target() trace.TargetIdentity   { return b.fixture.target }
func (*fixtureBinding) ProfileDigest() string            { return tracepreflight.Baseline().Digest() }
func (*fixtureBinding) Revalidate(context.Context) error { return nil }
func (*fixtureBinding) Close(context.Context) error      { return nil }
func (b *fixtureBinding) OpenStream(_ context.Context, deadline time.Time, identity nodebinding.StreamIdentity) (io.ReadCloser, error) {
	f := b.fixture
	if identity.StreamVersion != traceframe.AggregateVersion || identity.ProgrammeDigest != streamDigest {
		return nil, admission.ErrUnavailable
	}
	metadata, err := traceframe.NewMetadataVersion(traceframe.Metadata{SessionID: f.id, EngineDigest: identity.EngineDigest, ProgrammeDigest: streamDigest, Specification: f.spec, SessionStartedAt: time.Now().UTC(), Deadline: deadline}, f.version)
	if err != nil {
		return nil, err
	}
	data, err := traceframe.Encode(metadata)
	if err != nil {
		return nil, err
	}
	if f.truncated {
		return io.NopCloser(bytes.NewReader(data)), nil
	}
	acc, err := traceaggregate.New(trace.Files, f.spec.Bounds().Events)
	if err != nil {
		return nil, err
	}
	snapshot := acc.Snapshot()
	last, err := traceframe.NewSummaryVersion(traceframe.Summary{SessionEndedAt: time.Now().UTC(), Termination: trace.Cancelled, WrittenBytesBeforeSummary: uint64(len(data)), Incomplete: true, Aggregates: &snapshot}, traceframe.AggregateVersion)
	if err != nil {
		return nil, err
	}
	end, err := traceframe.Encode(last)
	return io.NopCloser(bytes.NewReader(append(data, end...))), err
}

func TestAggregateProxyPinsVersionAndPreservesTerminalOnTruncation(t *testing.T) {
	for _, tc := range []struct {
		name      string
		version   int
		truncated bool
		status    int
		terminal  string
	}{
		{"complete", traceframe.AggregateVersion, false, 200, `"fileAggregates"`},
		{"truncated", traceframe.AggregateVersion, true, 200, `"termination":"engine_failed"`},
		{"downgrade", traceframe.Version, true, 409, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &streamFixture{version: tc.version, truncated: tc.truncated, target: trace.TargetIdentity{Namespace: "tenant", PodName: "pod", PodUID: "uid", ContainerName: "worker", ContainerID: strings.Repeat("a", 64), ContainerStartedAt: time.Unix(10, 0).UTC(), NodeUID: "node", CgroupID: 42}}
			manager, err := admission.NewManager(context.Background(), admission.Dependencies{Authorizer: f, Resolver: f, Binder: f, Audit: func(admission.AuditEvent) {}}, admission.DefaultPolicy())
			if err != nil {
				t.Fatal(err)
			}
			defer manager.Close(context.Background())
			intent, err := admission.DecodeRequest("tenant", strings.NewReader(`{"schemaVersion":1,"pod":"pod","container":"worker","kind":"files"}`))
			if err != nil {
				t.Fatal(err)
			}
			principal := &user.DefaultInfo{Name: "fixture", Groups: []string{user.AllAuthenticated}}
			a, err := manager.Admit(context.Background(), principal, intent)
			if err != nil {
				t.Fatal(err)
			}
			lease, err := manager.Claim(context.Background(), principal, "tenant", a.ID())
			if err != nil {
				t.Fatal(err)
			}
			defer lease.Close(context.Background())
			proxy, err := NewStreamProxyVersion(map[trace.Kind]string{trace.Files: streamDigest}, traceframe.AggregateVersion)
			if err != nil {
				t.Fatal(err)
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if err := proxy.serve(r.Context(), w, lease); err != nil {
					writeError(w, err)
				}
			}))
			defer server.Close()
			response, err := server.Client().Get(server.URL)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			data, err := io.ReadAll(io.LimitReader(response.Body, 16384))
			if err != nil || response.StatusCode != tc.status {
				t.Fatalf("unexpected transport outcome: status %d, error %v", response.StatusCode, err)
			}
			if tc.status != 200 {
				return
			}
			if !strings.Contains(string(data), tc.terminal) {
				t.Fatal("terminal outcome lost")
			}
			reader := traceframe.NewReader(bytes.NewReader(data))
			for range 2 {
				frame, err := reader.Next()
				if err != nil || frame.Version() != traceframe.AggregateVersion {
					t.Fatal("proxy mixed or corrupted frame versions")
				}
			}
			if _, err := reader.Next(); err != io.EOF {
				t.Fatal("unexpected extra output")
			}
		})
	}
}

func TestAggregateProxyRejectsUnsupportedKindAndVersion(t *testing.T) {
	if _, err := NewStreamProxyVersion(map[trace.Kind]string{trace.OOM: streamDigest}, traceframe.AggregateVersion); err == nil {
		t.Fatal("v2 accepted unsupported OOM contract")
	}
	if _, err := NewStreamProxyVersion(nil, 0); err == nil {
		t.Fatal("missing version accepted")
	}
}
