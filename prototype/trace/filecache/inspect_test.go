package filecache

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/btf"
	"github.com/danushkastanley/kube-memlens/internal/trace"
)

func TestInspectRejectsMalformedObjects(t *testing.T) {
	for _, data := range [][]byte{nil, []byte("not ELF"), make([]byte, MaxObjectBytes+1)} {
		if _, err := Inspect(trace.Files, data); err == nil {
			t.Fatal("invalid object accepted")
		}
	}
}

// The separately compiled candidates are inspected without loading any BPF.
func TestCompiledCandidates(t *testing.T) {
	root := os.Getenv("KML_FILECACHE_OBJECTS")
	if root == "" {
		t.Skip("requires offline build_filecache.py output")
	}
	for _, arch := range []string{"amd64", "arm64"} {
		for _, kind := range []trace.Kind{trace.Files, trace.Cache} {
			t.Run(string(kind)+"/"+arch, func(t *testing.T) {
				data, err := os.ReadFile(filepath.Join(root, string(kind)+"-"+arch+"-1", "program.bpf.o"))
				if err != nil {
					t.Fatal(err)
				}
				spec, err := ebpf.LoadCollectionSpecFromReader(bytes.NewReader(data))
				if err != nil {
					t.Fatal(err)
				}
				name := "file_event"
				if kind == trace.Cache {
					name = "cache_event"
				}
				var record *btf.Struct
				if err := spec.Types.TypeByName(name, &record); err != nil {
					t.Fatal(err)
				}
				inventory, err := Inspect(kind, data)
				if err != nil {
					t.Fatal(err)
				}
				expectedSize, expectedHooks := uint32(552), 5
				if kind == trace.Cache {
					expectedSize, expectedHooks = 24, 2
				}
				if inventory.EventSize != expectedSize || len(inventory.Hooks) != expectedHooks {
					t.Fatal("unexpected event ABI or attachment inventory")
				}
				if err := ValidateObject(kind, data); err != nil {
					t.Fatal("candidate violates the frozen object policy")
				}
				inventory.Maps[0].MaxEntries++
				if validatePolicy(kind, inventory, spec) == nil {
					t.Fatal("expanded map accepted")
				}
			})
		}
	}
}
