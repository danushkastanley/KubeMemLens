package incident

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/model"
)

func deepBundle(schema int) api.IncidentBundle {
	return api.IncidentBundle{
		SchemaVersion: schema, ToolVersion: "v1.0.4",
		Pods: []api.PodSnapshot{{
			Namespace: "shop", PodName: "checkout", NodeName: "node-a",
			Context: api.PodContext{OwnerKind: "ReplicaSet", OwnerName: "checkout-7d9", Labels: map[string]string{"app": "checkout"}},
			Memory:  model.MemoryBreakdown{Name: "shop/checkout", TotalBytes: 1024},
			Containers: []api.ContainerSnapshot{{
				Namespace: "shop", PodName: "checkout", ContainerName: "app", ContainerID: "id-a", NodeName: "node-a",
				Context: api.ContainerContext{LastTerminationKnown: true, LastTerminationReason: "OOMKilled"},
			}},
		}},
		Nodes: []api.NodeSnapshotStatus{{NodeName: "node-a", Environment: api.NodeEnvironment{ContainerRuntimes: []string{"containerd"}}}},
	}
}

func readBundle(t *testing.T, bundle api.IncidentBundle) (Document, error) {
	t.Helper()
	body, err := json.Marshal(bundle)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "capture.json")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	return Read(path)
}

// An incident file is shared in tickets and read on someone else's terminal,
// so its text is untrusted. Deep bundles print names directly, and must reject
// terminal controls as the restricted, Node and volume formats already do.
func TestDeepBundleRejectsTerminalControlsInText(t *testing.T) {
	const clearScreen = "\x1b[2J\x1b[31mPWNED"
	tests := map[string]func(*api.IncidentBundle){
		"Pod name":       func(b *api.IncidentBundle) { b.Pods[0].PodName = "checkout" + clearScreen },
		"namespace":      func(b *api.IncidentBundle) { b.Pods[0].Namespace = "shop" + clearScreen },
		"Pod node name":  func(b *api.IncidentBundle) { b.Pods[0].NodeName = "node-a\r" },
		"owner name":     func(b *api.IncidentBundle) { b.Pods[0].Context.OwnerName = "rs" + clearScreen },
		"label value":    func(b *api.IncidentBundle) { b.Pods[0].Context.Labels["app"] = "x" + clearScreen },
		"label key":      func(b *api.IncidentBundle) { b.Pods[0].Context.Labels["a\x07pp"] = "x" },
		"memory name":    func(b *api.IncidentBundle) { b.Pods[0].Memory.Name = "shop/checkout\n" },
		"container name": func(b *api.IncidentBundle) { b.Pods[0].Containers[0].ContainerName = "app" + clearScreen },
		"termination reason": func(b *api.IncidentBundle) {
			b.Pods[0].Containers[0].Context.LastTerminationReason = "OOM\x1b]0;title\x07"
		},
		"node name":            func(b *api.IncidentBundle) { b.Nodes[0].NodeName = "node-a" + clearScreen },
		"container runtime":    func(b *api.IncidentBundle) { b.Nodes[0].Environment.ContainerRuntimes[0] = "containerd\x1b[0m" },
		"tool version":         func(b *api.IncidentBundle) { b.ToolVersion = "v1" + clearScreen },
		"history Pod name":     func(b *api.IncidentBundle) { b.Histories = []api.PodHistory{{PodName: "checkout\x1b[2J"}} },
		"bidirectional format": func(b *api.IncidentBundle) { b.Pods[0].PodName = "checkout‮gnp.exe" },
	}
	for _, schema := range []int{api.LegacySchemaVersion, api.CurrentIncidentSchemaVersion} {
		for name, corrupt := range tests {
			t.Run(name, func(t *testing.T) {
				bundle := deepBundle(schema)
				corrupt(&bundle)
				if _, err := readBundle(t, bundle); err == nil || !strings.Contains(err.Error(), "control") {
					t.Fatalf("schema %d: Read error = %v, want a control-character rejection", schema, err)
				}
				if err := ValidateSchema(bundle); err == nil {
					t.Fatalf("schema %d: a bundle with terminal controls can still be written", schema)
				}
			})
		}
	}
}

func TestDeepBundleAcceptsOrdinaryTextAndQuotedCaveats(t *testing.T) {
	for _, schema := range []int{api.LegacySchemaVersion, api.CurrentIncidentSchemaVersion} {
		bundle := deepBundle(schema)
		bundle.Pods[0].Context.Labels["app.kubernetes.io/name"] = "checkout-größe-日本"
		// Caveats are printed quoted, so controls in them are shown, not acted on.
		bundle.Caveats = []string{"untrusted\x1b[31m\nstatus"}
		document, err := readBundle(t, bundle)
		if err != nil || document.Deep == nil || len(document.Deep.Caveats) != 1 {
			t.Fatalf("schema %d: Read = %+v, %v", schema, document.Deep, err)
		}
	}
}

// Whatever the decoder accepts as a deep bundle must be safe to print: no text
// outside the quoted caveats may carry a C0 or C1 control or DEL.
func FuzzDeepIncidentText(f *testing.F) {
	for _, schema := range []int{api.LegacySchemaVersion, api.CurrentIncidentSchemaVersion} {
		clean, _ := json.Marshal(deepBundle(schema))
		f.Add(clean)
		hostile := deepBundle(schema)
		hostile.Pods[0].PodName = "checkout\x1b[2J"
		crafted, _ := json.Marshal(hostile)
		f.Add(crafted)
	}
	f.Add([]byte(`{"schemaVersion":1,"pods":[{"podName":"a\u009bb"}],"nodes":[]}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 64<<10 {
			t.Skip()
		}
		var bundle api.IncidentBundle
		if decodeStrict(data, &bundle) != nil || ValidateSchema(bundle) != nil {
			return
		}
		bundle.Caveats = nil
		body, err := json.Marshal(bundle)
		if err != nil {
			t.Fatal(err)
		}
		var generic any
		if err := json.Unmarshal(body, &generic); err != nil {
			t.Fatal(err)
		}
		if text, unsafe := firstControlText(generic); unsafe {
			t.Fatalf("accepted deep bundle carries control text %q", text)
		}
	})
}

func firstControlText(value any) (string, bool) {
	switch typed := value.(type) {
	case string:
		for _, r := range typed {
			if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
				return typed, true
			}
		}
	case []any:
		for _, item := range typed {
			if text, unsafe := firstControlText(item); unsafe {
				return text, true
			}
		}
	case map[string]any:
		for key, item := range typed {
			if text, unsafe := firstControlText(key); unsafe {
				return text, true
			}
			if text, unsafe := firstControlText(item); unsafe {
				return text, true
			}
		}
	}
	return "", false
}
