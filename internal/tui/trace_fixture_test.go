package tui

import (
	"bytes"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/trace"
	"github.com/danushkastanley/kube-memlens/internal/traceadmission"
	"github.com/danushkastanley/kube-memlens/internal/traceaggregate"
	"github.com/danushkastanley/kube-memlens/internal/traceclient"
	"github.com/danushkastanley/kube-memlens/internal/traceframe"
	"github.com/danushkastanley/kube-memlens/internal/tracepreflight"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
)

const traceTUITestID = "cccccccccccccccccccccccccccccccc"
const traceTUITestAPI = "/apis/tracing.kubememlens.io/v1alpha1"

type tuiTraceFixture struct {
	creates, streams, deletes atomic.Int64
	client                    *traceclient.Client
}

func traceTUIFixture(t *testing.T) *tuiTraceFixture {
	t.Helper()
	f := &tuiTraceFixture{}
	b := trace.DefaultBounds()
	b.Events = 100000
	b.OutputBytes = 32 << 20
	started := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	target := trace.TargetIdentity{Namespace: "tenant-a", PodName: "selected-pod", PodUID: "private-pod-uid", ContainerName: "worker", ContainerID: strings.Repeat("a", 64), ContainerStartedAt: started, NodeUID: "private-node-uid", CgroupID: 42}
	spec, err := trace.NewSpecification(trace.Files, target, trace.ConfirmedPaths, b)
	if err != nil {
		t.Fatal(err)
	}
	p := tracepreflight.Baseline()
	baseline := tracepreflight.Report{SchemaVersion: 1, Scope: p.Scope, ProfileDigest: p.Digest(), TraceApproval: "pending-custom-programme-freeze", CapturedAt: time.Now().Add(-time.Hour), State: tracepreflight.Supported}
	for _, id := range p.Checks {
		baseline.Checks = append(baseline.Checks, tracepreflight.Check{ID: id, State: tracepreflight.Supported, Reason: tracepreflight.Available})
	}
	programme := "sha256:" + strings.Repeat("b", 64)
	now := time.Now().UTC()
	end := now.Add(b.Duration)
	metadata, err := traceframe.NewMetadataVersion(traceframe.Metadata{SessionID: traceTUITestID, EngineDigest: p.EngineDigest, ProgrammeDigest: programme, Specification: spec, SessionStartedAt: now, Deadline: end}, 2)
	if err != nil {
		t.Fatal(err)
	}
	first, err := traceframe.Encode(metadata)
	if err != nil {
		t.Fatal(err)
	}
	agg, _ := traceaggregate.New(trace.Files, b.Events)
	var events bytes.Buffer
	path, _ := trace.NewSensitiveText("private-fixture-path", b.PathBytes)
	for range 100000 {
		event := trace.FileActivity{ObservedAt: now.Add(time.Millisecond), Operation: trace.FileRead, Path: path}
		frame, err := traceframe.NewFileVersion(event, spec, 2)
		if err != nil {
			t.Fatal(err)
		}
		data, err := traceframe.Encode(frame)
		if err != nil {
			t.Fatal(err)
		}
		events.Write(data)
		if err := agg.File(event); err != nil {
			t.Fatal(err)
		}
	}
	aggregates := agg.Snapshot()
	count, zero := uint64(100000), uint64(0)
	last, err := traceframe.NewSummaryVersion(traceframe.Summary{SessionEndedAt: end, ObservationStartedAt: &now, ObservationEndedAt: &end, Termination: trace.EventLimit, EngineCounts: trace.Counts{Produced: &count, Sampled: &zero, Lost: &zero, Rejected: &zero}, WrittenEvents: count, WrittenBytesBeforeSummary: uint64(len(first) + events.Len()), Incomplete: true, Aggregates: &aggregates}, 2)
	if err != nil {
		t.Fatal(err)
	}
	terminal, err := traceframe.Encode(last)
	if err != nil {
		t.Fatal(err)
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == traceTUITestAPI:
			_ = json.NewEncoder(w).Encode(metav1.APIResourceList{TypeMeta: metav1.TypeMeta{Kind: "APIResourceList", APIVersion: "v1"}, GroupVersion: "tracing.kubememlens.io/v1alpha1", APIResources: []metav1.APIResource{
				{Name: "traces", Namespaced: true, Verbs: metav1.Verbs{"create", "get", "delete"}}, {Name: "traces/stream", Namespaced: true, Verbs: metav1.Verbs{"get"}}, {Name: "tracepreflights", Namespaced: true, Verbs: metav1.Verbs{"create"}},
			}})
		case r.URL.Path == "/api/v1/namespaces/tenant-a/pods/selected-pod":
			_ = json.NewEncoder(w).Encode(corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: "tenant-a", Name: "selected-pod", UID: "private-pod-uid"}, Spec: corev1.PodSpec{NodeName: "selected-node"}, Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{{Name: "worker", ContainerID: "containerd://" + target.ContainerID, State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{StartedAt: metav1.NewTime(started)}}}}}})
		case strings.HasSuffix(r.URL.Path, "/tracepreflights"):
			req, err := traceadmission.DecodeRequest("tenant-a", r.Body)
			if err != nil {
				http.Error(w, "invalid", 400)
				return
			}
			doc, err := traceadmission.NewPreflightDocument(req, traceadmission.NodePreflight{Baseline: baseline, EngineDigest: p.EngineDigest, ProgrammeDigest: programme, StreamVersion: 2}, time.Now())
			if err != nil {
				t.Error(err)
				http.Error(w, "invalid", 400)
				return
			}
			_ = json.NewEncoder(w).Encode(doc)
		case r.Method == "DELETE":
			f.deletes.Add(1)
			_ = json.NewEncoder(w).Encode(metav1.Status{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Status"}, Status: metav1.StatusSuccess, Code: 200})
		case strings.HasSuffix(r.URL.Path, "/stream"):
			f.streams.Add(1)
			w.Header().Set("Content-Type", "application/x-ndjson")
			_, _ = w.Write(first)
			_, _ = w.Write(events.Bytes())
			_, _ = w.Write(terminal)
		case strings.HasSuffix(r.URL.Path, "/traces") || strings.HasSuffix(r.URL.Path, "/traces/"+traceTUITestID):
			if r.Method == "POST" {
				f.creates.Add(1)
				w.WriteHeader(201)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"apiVersion": "tracing.kubememlens.io/v1alpha1", "kind": "TraceAdmission", "metadata": map[string]string{"name": traceTUITestID, "namespace": "tenant-a"}, "state": "admitted", "expiresAt": time.Now().Add(10 * time.Second), "engineDigest": p.EngineDigest})
		default:
			t.Error("unexpected request", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	})
	server := httptest.NewTLSServer(handler)
	t.Cleanup(server.Close)
	ca := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	f.client, err = traceclient.New(&rest.Config{Host: server.URL, TLSClientConfig: rest.TLSClientConfig{CAData: ca}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(f.client.Close)
	return f
}
