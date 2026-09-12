package incident

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/capability"
	"github.com/danushkastanley/kube-memlens/internal/nodeanalysis"
	"github.com/danushkastanley/kube-memlens/internal/nodecontext"
)

func nodeFixture(t testing.TB) NodeBundle {
	t.Helper()
	now := time.Date(2026, 9, 11, 16, 0, 0, 0, time.UTC)
	usage := uint64(100)
	o := nodecontext.Observation{NodeName: "worker-a", NodeUID: "private-node-uid", ReportedAt: now, Availability: capability.Available,
		Evidence: capability.Envelope{Source: nodecontext.Source, APIVersion: "v1alpha1", CapturedAt: now, ReceivedAt: now, Scope: capability.NodeScope, Freshness: capability.Fresh, Completeness: capability.Partial, Stability: capability.ImplementationSpecific},
		Stats:    &nodecontext.Stats{StartedAt: now.Add(-time.Hour), Provenance: nodecontext.Unknown, Memory: &nodecontext.Memory{CapturedAt: now, UsageBytes: &usage}},
		Context:  &nodecontext.KubernetesContext{CapturedAt: now, MemoryPressure: "False"}}
	a, err := nodeanalysis.Analyse(nodeanalysis.Input{Now: now, NodeName: o.NodeName, NodeUID: o.NodeUID, Current: &o, SourceAvailability: capability.Available, Access: nodeanalysis.ClusterPods,
		Cgroup: nodeanalysis.CgroupFrame{NodeUID: o.NodeUID, CapturedAt: now, Coverage: nodeanalysis.Complete, Containers: []nodeanalysis.Container{{ID: "private-container", Namespace: "private-namespace", PodName: "private-pod", PodUID: "private-pod-uid", ContainerName: "app", WorkloadKind: "Deployment", WorkloadName: "private-workload", Charge: nodeanalysis.Charges{Total: 10, Anon: 10}, CompositionConsistent: true}}}})
	if err != nil {
		t.Fatal(err)
	}
	b := NodeBundle{SchemaVersion: NodeSchemaVersion, CapturedAt: now, ToolVersion: "test", Evidence: api.NodeEvidence{Record: api.NodeContextRecord{NodeName: o.NodeName, NodeUID: o.NodeUID, ReceivedAt: now, Freshness: capability.Fresh, Report: &o, LastGood: &o}, Analysis: a},
		History: &api.NodeContextHistory{NodeName: o.NodeName, Generation: "private-generation", ResetAt: now.Add(-time.Hour), WindowSeconds: 900, Completeness: capability.Partial, Series: []api.NodeContextHistorySeries{{NodeUID: o.NodeUID, Points: []api.NodeContextHistoryPoint{{ReceivedAt: now, Observation: o}}}}}}
	if err := ValidateNode(b); err != nil {
		t.Fatal(err)
	}
	return b
}

func TestNodeCaptureRedactionRoundTripAndPrivateFile(t *testing.T) {
	b := nodeFixture(t)
	before, _ := json.Marshal(b)
	redacted, err := NewNode(b.Evidence, b.History, b.ToolVersion, b.CapturedAt, false)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "node.json")
	if err := WriteNode(io.Discard, path, false, redacted); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("capture mode", info, err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, private := range []string{"private-node-uid", "private-pod-uid", "private-pod", "private-workload", "private-namespace", "private-generation", "private-container"} {
		if bytes.Contains(body, []byte(private)) {
			t.Fatal("raw private identity retained", private)
		}
	}
	after, _ := json.Marshal(b)
	if !bytes.Equal(before, after) {
		t.Fatal("redaction mutated live evidence")
	}
	document, err := Read(path)
	if err != nil || document.Node == nil || document.Deep != nil || document.Restricted != nil {
		t.Fatal(document, err)
	}
	if err := CompatibleNodes(b, *document.Node); err != nil {
		t.Fatal("fingerprint lost same-instance comparison", err)
	}
	if err := WriteNode(io.Discard, path, false, redacted); err == nil {
		t.Fatal("capture overwrote without authorisation")
	}
	if err := WriteNode(io.Discard, path, true, redacted); err != nil {
		t.Fatal(err)
	}
}

func TestNodeValidationRejectsContradictions(t *testing.T) {
	for name, mutate := range map[string]func(*NodeBundle){
		"wrong schema":      func(b *NodeBundle) { b.SchemaVersion = 3 },
		"future evaluation": func(b *NodeBundle) { b.Evidence.Analysis.EvaluatedAt = b.CapturedAt.Add(time.Minute) },
		"wrong UID":         func(b *NodeBundle) { b.Evidence.Analysis.NodeUID = "other" },
		"node-only ranks":   func(b *NodeBundle) { b.Evidence.Analysis.ContributorAccess = nodeanalysis.NodeOnly },
		"invalid PSI":       func(b *NodeBundle) { v := 101.0; b.Evidence.Analysis.Rankings.Pods[0].PSIFullAvg10 = &v },
		"unqualified gap": func(b *NodeBundle) {
			v := uint64(2)
			b.Evidence.Analysis.OutsidePods.Bytes = &v
			b.Evidence.Analysis.OutsidePods.State = capability.Available
		},
		"too many rows":        func(b *NodeBundle) { b.Evidence.Analysis.Rankings.Limit = 101 },
		"unknown caveat":       func(b *NodeBundle) { b.Evidence.Analysis.Caveats = append(b.Evidence.Analysis.Caveats, "private-note") },
		"raw redacted UID":     func(b *NodeBundle) { b.Redacted = true },
		"mixed history":        func(b *NodeBundle) { b.History.Series[0].Points[0].Observation.NodeName = "another-node" },
		"history continuation": func(b *NodeBundle) { b.History.Continue = "unread-page" },
		"false complete":       func(b *NodeBundle) { b.History.Completeness = capability.Complete; b.History.CoverageLost = true },
		"future history":       func(b *NodeBundle) { b.History.Series[0].Points[0].ReceivedAt = b.CapturedAt.Add(time.Minute) },
		"too many points":      func(b *NodeBundle) { b.History.Series[0].Points = make([]api.NodeContextHistoryPoint, 62) },
		"controls":             func(b *NodeBundle) { b.ToolVersion = "test\x1b[2J" },
	} {
		t.Run(name, func(t *testing.T) {
			b := nodeFixture(t)
			mutate(&b)
			if err := ValidateNode(b); err == nil {
				t.Fatal("invalid incident accepted")
			}
		})
	}
}

func TestNodeWireRejectsAliasesDuplicatesAndNestedExcess(t *testing.T) {
	b := nodeFixture(t)
	body, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := decodeNode(body); err != nil {
		t.Fatal(err)
	}
	for name, modified := range map[string][]byte{
		"alias":             bytes.Replace(body, []byte(`"schemaVersion":4`), []byte(`"SchemaVersion":4`), 1),
		"duplicate":         bytes.Replace(body, []byte(`"schemaVersion":4`), []byte(`"schemaVersion":4,"schemaVersion":4`), 1),
		"unknown":           bytes.Replace(body, []byte(`"schemaVersion":4`), []byte(`"schemaVersion":4,"secret":"no"`), 1),
		"null required":     bytes.Replace(body, []byte(`"redacted":false`), []byte(`"redacted":null`), 1),
		"missing redaction": bytes.Replace(body, []byte(`"redacted":false,`), nil, 1),
		"trailing":          append(append([]byte{}, body...), []byte(` {}`)...),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := decodeNode(modified); err == nil {
				t.Fatal("invalid wire accepted")
			}
		})
	}
	b.Evidence.Analysis.Signals = make([]nodeanalysis.Signal, 17)
	excess, _ := json.Marshal(b)
	if _, err := decodeNode(excess); err == nil || !strings.Contains(err.Error(), "array limit") {
		t.Fatal("array not bounded before typed decoding", err)
	}
	if _, err := decodeNode(make([]byte, MaxNodeBytes+1)); err == nil {
		t.Fatal("oversized file accepted")
	}
}

func TestNodeComparisonRefusesRecreationBootAndStaleSamples(t *testing.T) {
	before := nodeFixture(t)
	for _, scenario := range []string{"replacement", "boot", "stale", "reversed"} {
		t.Run(scenario, func(t *testing.T) {
			after := nodeFixture(t)
			switch scenario {
			case "replacement":
				after.Evidence.Record.NodeUID = "new"
				after.Evidence.Analysis.NodeUID = "new"
				after.Evidence.Record.Report.NodeUID = "new"
				after.History = nil
			case "boot":
				after.Evidence.Record.Report.Stats.StartedAt = after.CapturedAt.Add(-time.Minute)
				after.History = nil
			case "stale":
				after.Evidence.Analysis.EvaluatedAt = after.CapturedAt.Add(time.Minute)
				after.CapturedAt = after.Evidence.Analysis.EvaluatedAt
			case "reversed":
				before.CapturedAt = before.CapturedAt.Add(time.Minute)
			}
			if err := CompatibleNodes(before, after); err == nil {
				t.Fatal("incompatible comparison accepted")
			}
		})
	}
}

func FuzzNodeIncidentDecoder(f *testing.F) {
	valid, err := json.Marshal(nodeFixture(f))
	if err != nil {
		f.Fatal(err)
	}
	f.Add(valid)
	f.Add([]byte(`{"schemaVersion":4}`))
	f.Add([]byte(`{"evidence":{"analysis":{"signals":[{}]}}}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<20 {
			t.Skip()
		}
		b, err := decodeNode(data)
		if err == nil {
			if err := ValidateNode(b); err != nil {
				t.Fatal("decoder bypassed validation", err)
			}
		}
	})
}
