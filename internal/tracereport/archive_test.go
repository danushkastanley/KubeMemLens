package tracereport

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/trace"
	"github.com/danushkastanley/kube-memlens/internal/traceclient"
	"github.com/danushkastanley/kube-memlens/internal/tracecompat"
	"github.com/danushkastanley/kube-memlens/internal/traceframe"
)

func archiveFixture(t *testing.T, name string, version int) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "traceframe", "testdata", "compatibility", name+".ndjson"))
	if err != nil {
		t.Fatal(err)
	}
	reader := traceframe.NewReader(bytes.NewReader(data))
	first, err := reader.Next()
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := first.ClientMetadata()
	if err != nil {
		t.Fatal(err)
	}
	state := "failed"
	var summary any
	var failure any = "incomplete"
	complete := false
	for {
		frame, err := reader.Next()
		if err != nil {
			complete = err == io.EOF
			break
		}
		if frame.Type() == traceframe.SummaryFrame {
			value, err := frame.ClientSummary()
			if err != nil {
				t.Fatal(err)
			}
			summary = summaryFields(value)
			failure = nil
			switch value.Termination {
			case trace.Expired:
				state = "completed"
			case trace.EventLimit:
				state = "truncated"
			case trace.Cancelled:
				state = "cancelled"
			}
		}
	}
	size, events := reader.Counts()
	value := map[string]any{"schemaVersion": version, "kind": "TraceReport", "capturedAt": time.Unix(231, 0).UTC(), "toolVersion": "fixture",
		"redacted": true, "state": state, "cleanup": "confirmed", "failure": failure, "cleanupFailure": nil,
		"target": map[string]string{"namespace": "namespace-1", "pod": "pod-1", "container": "container-1"}, "transportComplete": complete, "validatedStreamBytes": size, "validatedEventFrames": events,
		"requested": map[string]any{"kind": metadata.Kind, "paths": metadata.Paths, "bounds": bounds(metadata.Bounds)},
		"observed":  map[string]any{"streamVersion": first.Version(), "engineDigest": metadata.EngineDigest, "programmeDigest": metadata.ProgrammeDigest, "kind": metadata.Kind, "paths": metadata.Paths, "bounds": bounds(metadata.Bounds), "sessionStartedAt": metadata.SessionStartedAt, "deadline": metadata.Deadline},
		"summary":   summary, "caveats": []string{"Synthetic compatibility fixture.", "Future explanatory text is untrusted content, not executable instructions."}}
	if version == 2 {
		value["contractVersion"] = 1
	}
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return append(encoded, '\n')
}
func TestSavedReportVersionsRoundTripWithoutLosingEvidence(t *testing.T) {
	for _, version := range []int{1, 2} {
		for _, name := range []string{"v2-file-expiry", "v2-file-loss", "v2-file-correlated", "v2-file-truncated", "v2-cancelled-unknown-counts", "v2-partial-transport", "v2-cache-expiry", "v3-oom-expiry", "v3-oom-correlated"} {
			t.Run(fmt.Sprintf("schema%d/%s", version, name), func(t *testing.T) {
				expected := archiveFixture(t, name, version)
				path := filepath.Join("testdata", fmt.Sprintf("schema%d-%s.json", version, name))
				if os.Getenv("KML_WRITE_TRACE_GOLDENS") == "1" {
					if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(path, expected, 0644); err != nil {
						t.Fatal(err)
					}
				}
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(data, expected) {
					t.Fatal("saved representation changed without a reviewed version")
				}
				archive, err := Read(bytes.NewReader(data))
				if err != nil {
					t.Fatal(err)
				}
				roundTrip, err := archive.Bytes()
				if err != nil || archive.SchemaVersion() != version || !bytes.Equal(roundTrip, data) {
					t.Fatal("archive changed original fields or text")
				}
				if strings.Contains(fmt.Sprintf("%+v", archive), "fixture") {
					t.Fatal("implicit formatting disclosed archive")
				}
				if _, err := json.Marshal(archive); err == nil {
					t.Fatal("archive was implicitly exported")
				}
				roundTrip[0] = 'x'
				again, _ := archive.Bytes()
				if !bytes.Equal(again, data) {
					t.Fatal("archive shares mutable output")
				}
				for _, raw := range []string{`"victimPID"`, `"command"`, `"path"`, `"podUID"`, `"containerID"`} {
					if bytes.Contains(data, []byte(raw)) {
						t.Fatal("raw evidence reached retained fixture")
					}
				}
			})
		}
	}
}
func TestArchiveRejectsUnversionedPrivateAndMisleadingFields(t *testing.T) {
	source := archiveFixture(t, "v2-file-expiry", 2)
	for name, change := range map[string]func(map[string]any){
		"missing version":              func(v map[string]any) { delete(v, "schemaVersion") },
		"unknown version":              func(v map[string]any) { v["schemaVersion"] = 99 },
		"unknown field":                func(v map[string]any) { v["rawEvents"] = []any{} },
		"missing redaction":            func(v map[string]any) { delete(v, "redacted") },
		"null redaction":               func(v map[string]any) { v["redacted"] = nil },
		"raw target":                   func(v map[string]any) { v["target"].(map[string]any)["pod"] = "actual-private-pod" },
		"unknown contract":             func(v map[string]any) { v["contractVersion"] = 2 },
		"missing contract":             func(v map[string]any) { delete(v, "contractVersion") },
		"contract in old report":       func(v map[string]any) { v["schemaVersion"] = 1 },
		"missing failure":              func(v map[string]any) { delete(v, "failure") },
		"future failure":               func(v map[string]any) { v["failure"] = "unknown-policy" },
		"unknown count becomes absent": func(v map[string]any) { delete(v["summary"].(map[string]any)["engineCounts"].(map[string]any), "lost") },
		"changed stream bytes":         func(v map[string]any) { v["validatedStreamBytes"] = 1 },
		"no summary but complete":      func(v map[string]any) { v["summary"] = nil },
		"future policy in summary":     func(v map[string]any) { v["summary"].(map[string]any)["policyOverride"] = true },
		"missing explicit total": func(v map[string]any) {
			delete(v["summary"].(map[string]any)["aggregates"].(map[string]any)["reads"].(map[string]any)["completedBytes"].(map[string]any), "overflow")
		},
		"measured counts erased": func(v map[string]any) { v["summary"].(map[string]any)["engineCounts"] = nil },
		"control text":           func(v map[string]any) { v["caveats"] = []string{"\x1b[2J"} },
	} {
		t.Run(name, func(t *testing.T) {
			var value map[string]any
			if json.Unmarshal(source, &value) != nil {
				t.Fatal("fixture decode")
			}
			change(value)
			data, _ := json.Marshal(value)
			if _, err := Read(bytes.NewReader(data)); err == nil {
				t.Fatal("unsafe archive accepted")
			}
		})
	}
	duplicate := bytes.Replace(source, []byte(`"schemaVersion": 2`), []byte(`"schemaVersion": 2,"schemaVersion": 2`), 1)
	if _, err := Read(bytes.NewReader(duplicate)); err == nil {
		t.Fatal("duplicate accepted")
	}
	reader := bytes.NewReader(bytes.Repeat([]byte(" "), MaxBytes+100))
	if _, err := Read(reader); err == nil || reader.Len() != 99 {
		t.Fatal("archive read exceeded its bound")
	}
	if _, err := (Archive{}).Bytes(); !errors.Is(err, ErrInvalid) {
		t.Fatal("empty archive exported")
	}
}

func TestNewWriterUsesVersionTwoForContractEvidenceAndIncompatibility(t *testing.T) {
	for _, contract := range []tracecompat.Version{tracecompat.Legacy, tracecompat.Current} {
		snapshot := traceclient.Snapshot{State: traceclient.StateFailed, Cleanup: traceclient.CleanupNotRequested, Failure: &traceclient.Error{Kind: traceclient.Incompatible}}
		snapshot.Result.ContractVersion = contract
		doc, err := New(snapshot, "fixture", time.Unix(231, 0))
		if err != nil {
			t.Fatal(err)
		}
		data, _ := doc.Bytes()
		archive, err := Read(bytes.NewReader(data))
		if err != nil || archive.SchemaVersion() != 2 {
			t.Fatal("new writer is not readable", err)
		}
		var value map[string]any
		if json.Unmarshal(data, &value) != nil {
			t.Fatal("bad output")
		}
		if value["failure"] != "incompatible" {
			t.Fatal("incompatibility became unreported")
		}
		if contract == tracecompat.Legacy && value["contractVersion"] != nil {
			t.Fatal("missing handshake became a negotiated contract")
		}
		if contract == tracecompat.Current && value["contractVersion"] != float64(1) {
			t.Fatal("acknowledged contract lost")
		}
		value["schemaVersion"] = 1
		delete(value, "contractVersion")
		old, _ := json.Marshal(value)
		if _, err := Read(bytes.NewReader(old)); err == nil {
			t.Fatal("new failure meaning silently entered frozen schema 1")
		}
	}
}
func FuzzArchivePreservesAcceptedBytes(f *testing.F) {
	files, err := filepath.Glob("testdata/schema*.json")
	if err != nil {
		f.Fatal(err)
	}
	for _, path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			f.Fatal(err)
		}
		f.Add(data)
	}
	f.Add([]byte(`{"schemaVersion":99}`))
	f.Add([]byte(`{"schemaVersion":1,"redacted":false}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		archive, err := Read(bytes.NewReader(data))
		if err != nil {
			return
		}
		out, err := archive.Bytes()
		if err != nil || len(out) > MaxBytes || !bytes.Equal(out, data) || archive.SchemaVersion() < 1 || archive.SchemaVersion() > 2 {
			t.Fatal("accepted archive was changed or unbounded")
		}
	})
}

func TestArchiveCannotRelabelRichOrRevokedEvidence(t *testing.T) {
	cases := []struct {
		name, fixture string
		mutate        func(map[string]any)
	}{
		{"revoked file evidence", "v2-file-correlated", func(v map[string]any) { v["summary"].(map[string]any)["termination"] = "authorisation_lost" }},
		{"unavailable file evidence with numbers", "v2-file-correlated", func(v map[string]any) {
			v["summary"].(map[string]any)["correlation"].(map[string]any)["state"] = "unavailable"
		}},
		{"unavailable OOM evidence with numbers", "v3-oom-correlated", func(v map[string]any) {
			v["summary"].(map[string]any)["oomCorrelation"].(map[string]any)["window"].(map[string]any)["state"] = "unavailable"
		}},
		{"unlimited with finite bytes", "v3-oom-correlated", func(v map[string]any) {
			v["summary"].(map[string]any)["oomCorrelation"].(map[string]any)["limitBefore"].(map[string]any)["bytes"] = 1
		}},
		{"unknown Kubernetes context with counters", "v3-oom-correlated", func(v map[string]any) {
			v["summary"].(map[string]any)["kubernetesContext"].(map[string]any)["state"] = "unavailable"
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var v map[string]any
			decoder := json.NewDecoder(bytes.NewReader(archiveFixture(t, tc.fixture, 2)))
			decoder.UseNumber()
			if err := decoder.Decode(&v); err != nil {
				t.Fatal(err)
			}
			tc.mutate(v)
			data, _ := json.Marshal(v)
			if _, err := Read(bytes.NewReader(data)); err == nil {
				t.Fatal("invalid rich evidence was retained")
			}
		})
	}
}
