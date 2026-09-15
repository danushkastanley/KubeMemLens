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
	"github.com/danushkastanley/kube-memlens/prototype/trace/nodebinding"
	"k8s.io/apiserver/pkg/authentication/user"
)

// This fixture exercises real admission/proxy framing with a memory node stream.
// It does not stand in for kernel OOM or Kubernetes-provider qualification.
type oomStreamFixture struct {
	streamFixture
	contextReads                 int
	forgedContext, replacedAfter bool
}

func (f *oomStreamFixture) Bind(ctx context.Context, id string, workload admission.Workload, request admission.Request, expires time.Time) (admission.Binding, error) {
	if _, err := f.streamFixture.Bind(ctx, id, workload, request, expires); err != nil {
		return nil, err
	}
	return &oomFixtureBinding{fixtureBinding{&f.streamFixture}, f}, nil
}

type oomFixtureBinding struct {
	fixtureBinding
	oom *oomStreamFixture
}

func (f *oomStreamFixture) SampleOOMContext(_ context.Context, target trace.TargetIdentity) (trace.OOMKubernetesSample, error) {
	f.contextReads++
	if f.replacedAfter && f.contextReads == 2 {
		return trace.OOMKubernetesSample{}, admission.ErrTargetChanged
	}
	at, zero := time.Now().UTC(), uint64(0)
	return trace.OOMKubernetesSample{Target: target, Start: at, End: at, Restarts: &zero, NodePressure: "false"}, nil
}

func (b *oomFixtureBinding) OpenStream(_ context.Context, deadline time.Time, identity nodebinding.StreamIdentity) (io.ReadCloser, error) {
	f := b.oom
	if f.contextReads != 1 || identity.StreamVersion != traceframe.OOMVersion || identity.ProgrammeDigest != streamDigest {
		return nil, admission.ErrUnavailable
	}
	start := time.Now().UTC()
	metadata, err := traceframe.NewMetadataVersion(traceframe.Metadata{SessionID: f.id, EngineDigest: identity.EngineDigest, ProgrammeDigest: streamDigest, Specification: f.spec, SessionStartedAt: start, Deadline: deadline}, traceframe.OOMVersion)
	if err != nil {
		return nil, err
	}
	command, _ := trace.NewSensitiveText("fixture", 16)
	pid := uint32(1234)
	event := trace.OOMDecision{ObservedAt: time.Now().UTC(), Scope: trace.OOMScopeCgroup, VictimPID: &pid, Command: command}
	frame, err := traceframe.NewOOMVersion(event, f.spec, traceframe.OOMVersion)
	if err != nil {
		return nil, err
	}
	first, _ := traceframe.Encode(metadata)
	middle, _ := traceframe.Encode(frame)
	data := append(first, middle...)
	acc, _ := traceaggregate.New(trace.OOM, f.spec.Bounds().Events)
	if acc.OOM(event) != nil {
		return nil, admission.ErrUnavailable
	}
	aggregate := acc.Snapshot()
	end := time.Now().UTC()
	one, zero := uint64(1), uint64(0)
	summary := traceframe.Summary{SessionEndedAt: end, ObservationStartedAt: &start, ObservationEndedAt: &end, Termination: trace.Cancelled, EngineCounts: trace.Counts{Produced: &one, Sampled: &zero, Lost: &zero, Rejected: &zero}, WrittenEvents: 1, WrittenBytesBeforeSummary: uint64(len(data)), Incomplete: true, Aggregates: &aggregate}
	if f.forgedContext {
		summary.KubernetesContext = &trace.KubernetesOOMContext{State: "unavailable"}
	}
	last, err := traceframe.NewSummaryVersion(summary, traceframe.OOMVersion)
	if err != nil {
		return nil, err
	}
	final, _ := traceframe.Encode(last)
	return io.NopCloser(bytes.NewReader(append(data, final...))), nil
}

func TestOOMProxyEnrichesOnlyControlContextAndStopsReplacement(t *testing.T) {
	for _, scenario := range []string{"complete", "forged-node-context", "target-replaced"} {
		t.Run(scenario, func(t *testing.T) {
			f := &oomStreamFixture{streamFixture: streamFixture{target: oomContextSpec(t).Target()}, forgedContext: scenario == "forged-node-context", replacedAfter: scenario == "target-replaced"}
			manager, err := admission.NewManager(context.Background(), admission.Dependencies{Authorizer: f, Resolver: f, Binder: f, Audit: func(admission.AuditEvent) {}}, admission.DefaultPolicy())
			if err != nil {
				t.Fatal(err)
			}
			defer manager.Close(context.Background())
			intent, err := admission.DecodeRequest("tenant", strings.NewReader(`{"schemaVersion":1,"pod":"pod","container":"worker","kind":"oom"}`))
			if err != nil {
				t.Fatal(err)
			}
			principal := &user.DefaultInfo{Name: "fixture", Groups: []string{user.AllAuthenticated}}
			admitted, err := manager.Admit(context.Background(), principal, intent)
			if err != nil {
				t.Fatal(err)
			}
			lease, err := manager.Claim(context.Background(), principal, "tenant", admitted.ID())
			if err != nil {
				t.Fatal(err)
			}
			defer lease.Close(context.Background())
			proxy, err := NewStreamProxyVersion(map[trace.Kind]string{trace.OOM: streamDigest}, traceframe.OOMVersion)
			if err != nil {
				t.Fatal(err)
			}
			finished := make(chan error, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				err := proxy.WithOOMContext(f).serve(r.Context(), w, lease)
				finished <- err
				if err != nil {
					writeError(w, err)
				}
			}))
			defer server.Close()
			client := server.Client()
			client.Timeout = 2 * time.Second
			response, err := client.Get(server.URL)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			data, err := io.ReadAll(io.LimitReader(response.Body, 16384))
			if err != nil || response.StatusCode != http.StatusOK {
				t.Fatal("OOM HTTP stream failed")
			}
			if err := <-finished; err != nil {
				t.Fatal(err)
			}
			r := traceframe.NewReader(bytes.NewReader(data))
			for range 3 {
				if _, err := r.Next(); err != nil {
					t.Fatal("invalid OOM proxy stream")
				}
			}
			if _, err := r.Next(); err != io.EOF || f.contextReads != 2 {
				t.Fatal("context sampling or stream completion changed")
			}
			body := string(data)
			expected := map[string]string{"complete": `"kubernetesContext":{"state":"observed"`, "forged-node-context": `"termination":"engine_failed"`, "target-replaced": `"termination":"target_changed"`}[scenario]
			if !strings.Contains(body, expected) || (scenario != "complete" && strings.Contains(body, "kubernetesContext")) {
				t.Fatal("OOM context crossed its provenance or target boundary")
			}
		})
	}
}
