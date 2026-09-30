package tracereport

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/trace"
	"github.com/danushkastanley/kube-memlens/internal/traceclient"
	"github.com/danushkastanley/kube-memlens/internal/traceframe"
)

func TestReportOmitsPrivateIdentityAndUntrustedErrors(t *testing.T) {
	private := "sensitive-never-export"
	s := traceclient.Snapshot{State: traceclient.StateFailed, AdmissionID: private,
		Selection: traceclient.Selection{Namespace: private, Pod: private, PodUID: private, Container: private, ContainerID: private, NodeName: private},
		Intent:    traceclient.DefaultIntent(trace.Files), Cleanup: traceclient.CleanupUnconfirmed,
		Failure: errors.New(private), CleanupFailure: errors.New(private)}
	s.Result.Metadata = traceframe.ClientMetadata{SessionID: private, Namespace: private, Pod: private, PodUID: private, Container: private}
	s.Result.StreamVersion = 2
	s.Result.Metadata.Kind, s.Result.Metadata.Paths, s.Result.Metadata.Bounds = trace.Files, trace.OmitPaths, trace.DefaultBounds()
	s.Result.Metadata.EngineDigest, s.Result.Metadata.ProgrammeDigest = "sha256:"+strings.Repeat("a", 64), "sha256:"+strings.Repeat("b", 64)
	s.Result.Metadata.SessionStartedAt = time.Now()
	s.Result.Metadata.Deadline = s.Result.Metadata.SessionStartedAt.Add(30 * time.Second)
	doc, err := New(s, "test", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	data, err := doc.Bytes()
	if err != nil || len(data) > MaxBytes || strings.Contains(string(data), private) {
		t.Fatal("private data leaked or export exceeded bounds")
	}
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["summary"] != nil || decoded["failure"] != "unreported" || decoded["cleanup"] != "unconfirmed" || decoded["redacted"] != true {
		t.Fatal("missing evidence or cleanup became success")
	}
	if !strings.Contains(string(data), "unknown") || !strings.Contains(string(data), "qualification") {
		t.Fatal("missing caveats")
	}
	data[0] = 'x'
	s.Intent.Kind = trace.OOM
	again, _ := doc.Bytes()
	if !json.Valid(again) || strings.Contains(string(again), `"kind":"oom"`) {
		t.Fatal("document shares mutable input or output")
	}
}

func TestReportRequiresTerminalStateAndValidVersion(t *testing.T) {
	for _, state := range []traceclient.State{traceclient.StateNew, traceclient.StateReady, traceclient.StateRunning, traceclient.StateCancelling, "unknown"} {
		if _, err := New(traceclient.Snapshot{State: state}, "test", time.Now()); !errors.Is(err, ErrInvalid) {
			t.Fatal("accepted non-terminal report", state)
		}
	}
	for _, version := range []string{"", strings.Repeat("x", 257), "v1\nsecret", "\x1b[31m"} {
		if _, err := New(traceclient.Snapshot{State: traceclient.StateFailed}, version, time.Now()); !errors.Is(err, ErrInvalid) {
			t.Fatal("accepted invalid version")
		}
	}
	if _, err := (Document{}).Bytes(); !errors.Is(err, ErrInvalid) {
		t.Fatal("empty document accepted")
	}
}

func TestSummaryProjectionRetainsUnknownCountsAndTermination(t *testing.T) {
	s := traceframe.Summary{Termination: trace.EventLimit, Incomplete: true}
	data, err := json.Marshal(summaryFields(s))
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Termination  string             `json:"termination"`
		Incomplete   bool               `json:"incomplete"`
		EngineCounts map[string]*uint64 `json:"engineCounts"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Termination != string(trace.EventLimit) || !decoded.Incomplete || len(decoded.EngineCounts) != 4 {
		t.Fatal("summary lost termination or uncertainty")
	}
	for _, count := range decoded.EngineCounts {
		if count != nil {
			t.Fatal("unknown count became zero")
		}
	}
}
