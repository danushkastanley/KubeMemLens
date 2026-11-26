package workeripc

import (
	"bytes"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/trace"
)

func messages(t *testing.T, values ...responseWire) []byte {
	t.Helper()
	var data []byte
	for _, value := range values {
		encoded, err := encode(value)
		if err != nil {
			t.Fatal(err)
		}
		data = append(data, encoded...)
	}
	return data
}

func TestReaderRejectsInvalidStatesBeforeForwarding(t *testing.T) {
	r := requestFixture(t, trace.Files, trace.OmitPaths, trace.DefaultBounds())
	ready := responseWire{Version: Version, Type: "ready"}
	file := responseWire{Version: Version, Type: "file", File: &fileWire{r.IssuedAt, trace.FileRead, 10, 5, ""}}
	finished := responseWire{Version: Version, Type: "result", Result: &resultWire{Termination: trace.Expired, Incomplete: true}}
	for name, values := range map[string][]responseWire{
		"event-before-ready":   {file},
		"ready-twice":          {ready, ready},
		"success-before-ready": {finished},
		"data-after-result":    {ready, finished, file},
		"ready-payload":        {{Version: Version, Type: "ready", File: file.File}},
		"wrong-kind":           {ready, {Version: Version, Type: "cache", Cache: &cacheWire{r.IssuedAt, trace.CacheAdd, 1}}},
		"omitted-terminal":     {ready},
		"unsupported-version":  {{Version: Version + 1, Type: "ready"}},
	} {
		t.Run(name, func(t *testing.T) {
			var output capture
			if _, err := ReadStream(bytes.NewReader(messages(t, values...)), r, &output, func() {}); err != ErrProtocol {
				t.Fatal("invalid state accepted")
			}
			if len(output.files) != 0 || len(output.cache) != 0 {
				t.Fatal("invalid state forwarded data")
			}
		})
	}
}

func TestReaderAndWriterRejectInvalidFileSemantics(t *testing.T) {
	r := requestFixture(t, trace.Files, trace.OmitPaths, trace.DefaultBounds())
	ready := responseWire{Version: Version, Type: "ready"}
	for name, event := range map[string]fileWire{
		"path-without-consent":     {r.IssuedAt, trace.FileRead, 10, 5, "/secret"},
		"completed-over-requested": {r.IssuedAt, trace.FileRead, 10, 11, ""},
		"unsupported-open":         {r.IssuedAt, trace.FileOpen, 0, 0, ""},
		"too-old":                  {r.IssuedAt.Add(-time.Second), trace.FileRead, 10, 5, ""},
		"too-new":                  {r.Deadline.Add(time.Second), trace.FileRead, 10, 5, ""},
	} {
		t.Run(name, func(t *testing.T) {
			var output capture
			data := messages(t, ready, responseWire{Version: Version, Type: "file", File: &event})
			if _, err := ReadStream(bytes.NewReader(data), r, &output, func() {}); err != ErrProtocol || len(output.files) != 0 {
				t.Fatal("invalid event crossed private boundary")
			}
			var sink bytes.Buffer
			w, _ := NewWriter(&sink, r)
			if err := w.Ready(); err != nil {
				t.Fatal(err)
			}
			path, _ := trace.NewSensitiveText(event.Path, 256)
			if err := w.FileActivity(trace.FileActivity{ObservedAt: event.ObservedAt, Operation: event.Operation, RequestedBytes: &event.Requested, CompletedBytes: &event.Completed, Path: path}); err != ErrProtocol {
				t.Fatal("invalid event encoded")
			}
		})
	}
}

func TestReaderEnforcesIndependentByteAndEventBudgets(t *testing.T) {
	for _, limit := range []string{"events", "bytes"} {
		t.Run(limit, func(t *testing.T) {
			bounds := trace.DefaultBounds()
			bounds.Events = 1
			if limit == "bytes" {
				bounds.OutputBytes = 1
			}
			r := requestFixture(t, trace.Files, trace.OmitPaths, bounds)
			file := responseWire{Version: Version, Type: "file", File: &fileWire{r.IssuedAt, trace.FileRead, 10, 5, ""}}
			data := messages(t, responseWire{Version: Version, Type: "ready"}, file, file)
			var output capture
			if _, err := ReadStream(bytes.NewReader(data), r, &output, func() {}); err != ErrProtocol {
				t.Fatal("reader trusted writer limits")
			}
			want := 1
			if limit == "bytes" {
				want = 0
			}
			if len(output.files) != want {
				t.Fatal("extra event delivered")
			}
		})
	}
}

func TestResultCountsAndWindowsMustBeConsistent(t *testing.T) {
	r := requestFixture(t, trace.Files, trace.OmitPaths, trace.DefaultBounds())
	zero, one, maximum := uint64(0), uint64(1), uint64(math.MaxUint64)
	for name, result := range map[string]resultWire{
		"partial-counts":     {Termination: trace.Expired, Produced: &one, Incomplete: true},
		"overflow":           {Termination: trace.Expired, Produced: &maximum, Sampled: &maximum, Lost: &one, Rejected: &zero, Incomplete: true},
		"invalid-total":      {Termination: trace.Expired, Produced: &zero, Sampled: &zero, Lost: &one, Rejected: &zero, Incomplete: true},
		"half-window":        {StartedAt: r.IssuedAt, Termination: trace.Expired, Incomplete: true},
		"reversed-window":    {StartedAt: r.Deadline, EndedAt: r.IssuedAt, Termination: trace.Expired, Incomplete: true},
		"false-completeness": {Termination: trace.Expired},
		"unknown-reason":     {Termination: "success", Incomplete: true},
	} {
		t.Run(name, func(t *testing.T) {
			data := messages(t, responseWire{Version: Version, Type: "ready"}, responseWire{Version: Version, Type: "result", Result: &result})
			if _, err := ReadStream(bytes.NewReader(data), r, &capture{}, func() {}); err != ErrProtocol {
				t.Fatal("invalid result accepted")
			}
		})
	}
}

func TestOutputFailureIsRedactedAndStopsDelivery(t *testing.T) {
	r := requestFixture(t, trace.Files, trace.OmitPaths, trace.DefaultBounds())
	file := responseWire{Version: Version, Type: "file", File: &fileWire{r.IssuedAt, trace.FileRead, 10, 5, ""}}
	output := &capture{fail: true}
	_, err := ReadStream(bytes.NewReader(messages(t, responseWire{Version: Version, Type: "ready"}, file, file)), r, output, func() {})
	if !errors.Is(err, ErrOutput) || strings.Contains(err.Error(), "private output error") || len(output.files) != 0 {
		t.Fatal("output failure not bounded/redacted")
	}
}

func TestStartupFailureCanFinishWithoutReadiness(t *testing.T) {
	r := requestFixture(t, trace.Files, trace.OmitPaths, trace.DefaultBounds())
	var data bytes.Buffer
	w, _ := NewWriter(&data, r)
	result := trace.Result{Version: trace.ContractVersion, Termination: trace.EngineFailed, Incomplete: true}
	if err := w.Finish(result); err != nil {
		t.Fatal(err)
	}
	ready := false
	got, err := ReadStream(&data, r, &capture{}, func() { ready = true })
	if err != nil || ready || got.Termination != trace.EngineFailed || got.Counts.Produced != nil {
		t.Fatal("startup failure misreported")
	}
}

func TestFinalCountsCannotContradictDeliveredEvents(t *testing.T) {
	r := requestFixture(t, trace.Files, trace.OmitPaths, trace.DefaultBounds())
	zero := uint64(0)
	result := resultWire{Termination: trace.Expired, Produced: &zero, Sampled: &zero, Lost: &zero, Rejected: &zero, Incomplete: true}
	data := messages(t, responseWire{Version: Version, Type: "ready"}, responseWire{Version: Version, Type: "file", File: &fileWire{r.IssuedAt, trace.FileRead, 1, 1, ""}}, responseWire{Version: Version, Type: "result", Result: &result})
	var output capture
	if _, err := ReadStream(bytes.NewReader(data), r, &output, func() {}); err != ErrProtocol || len(output.files) != 1 {
		t.Fatal("counter contradiction accepted")
	}
}
