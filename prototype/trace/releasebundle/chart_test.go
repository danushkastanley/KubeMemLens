package releasebundle

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	sigsyaml "sigs.k8s.io/yaml"
)

func chartFixture(t testing.TB, change func(map[string][]byte)) ([]byte, ImageIdentity) {
	t.Helper()
	files := make(map[string][]byte)
	root := "../../../charts/kube-memlens-trace"
	if err := filepath.WalkDir(root, func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(root, name)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(name)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(relative)] = data
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	identity := ImageIdentity{Version: "0.1.0-dev.1", SourceCommit: strings.Repeat("a", 40), IndexDigest: "sha256:" + strings.Repeat("b", 64)}
	updateYAML(t, files, "Chart.yaml", func(value map[string]any) { value["version"], value["appVersion"] = identity.Version, identity.Version })
	updateYAML(t, files, "values.yaml", func(value map[string]any) {
		image := value["image"].(map[string]any)
		image["repository"], image["digest"] = imageRepository, identity.IndexDigest
	})
	if change != nil {
		change(files)
	}
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	members := make([]member, 0, len(names))
	for _, name := range names {
		members = append(members, file(chartRoot+name, string(files[name])))
	}
	return compressed(t, archive(t, members...)), identity
}
func updateYAML(t testing.TB, files map[string][]byte, name string, change func(map[string]any)) {
	t.Helper()
	encoded, err := sigsyaml.YAMLToJSONStrict(files[name])
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	if json.Unmarshal(encoded, &value) != nil {
		t.Fatal("invalid fixture")
	}
	change(value)
	files[name], err = json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
}
func TestChartBindsReleaseAndImageWhileRemainingDisabled(t *testing.T) {
	raw, identity := chartFixture(t, nil)
	chart, err := InspectChart(bytes.NewReader(raw), int64(len(raw)), sum(raw), identity)
	if err != nil || chart.Version() != identity.Version || chart.ImageReference() != imageRepository+"@"+identity.IndexDigest {
		t.Fatal("valid chart refused", err)
	}
}
func TestChartRefusesUnsafeDefaultsAndMismatchedIdentity(t *testing.T) {
	changes := map[string]func(map[string]any){
		"enabled":               func(v map[string]any) { v["enabled"] = true },
		"quoted boolean":        func(v map[string]any) { v["enabled"] = "false" },
		"implicit consent":      func(v map[string]any) { v["acknowledgeUnqualifiedDevelopment"] = true },
		"profile":               func(v map[string]any) { v["profile"] = "development-linux-containerd" },
		"wrong image":           func(v map[string]any) { v["image"].(map[string]any)["repository"] = "example.invalid/unreviewed" },
		"mutable image":         func(v map[string]any) { v["image"].(map[string]any)["digest"] = "latest" },
		"other digest":          func(v map[string]any) { v["image"].(map[string]any)["digest"] = "sha256:" + strings.Repeat("c", 64) },
		"prepopulated identity": func(v map[string]any) { v["nodes"] = []any{map[string]any{"name": "private"}} },
		"wider limit":           func(v map[string]any) { v["maxNodeTraces"] = 2 },
		"path disclosure":       func(v map[string]any) { v["allowConfirmedPaths"] = true },
		"missing value":         func(v map[string]any) { delete(v, "acceptancePolicySHA256") },
		"unknown value":         func(v map[string]any) { v["extraArgs"] = []string{"--unsafe"} },
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			raw, identity := chartFixture(t, func(files map[string][]byte) { updateYAML(t, files, "values.yaml", change) })
			if _, err := InspectChart(bytes.NewReader(raw), int64(len(raw)), sum(raw), identity); !errors.Is(err, ErrArchive) {
				t.Fatal("unsafe chart defaults accepted")
			}
		})
	}
	for _, key := range []string{"name", "version", "appVersion", "apiVersion", "type", "home"} {
		raw, identity := chartFixture(t, func(files map[string][]byte) {
			updateYAML(t, files, "Chart.yaml", func(v map[string]any) { v[key] = "different" })
		})
		if _, err := InspectChart(bytes.NewReader(raw), int64(len(raw)), sum(raw), identity); !errors.Is(err, ErrArchive) {
			t.Fatal("mismatched chart metadata accepted")
		}
	}
}
func TestChartRejectsAmbiguousYAMLHiddenSubchartsAndSchemaReferences(t *testing.T) {
	cases := map[string]func(map[string][]byte){
		"second document": func(f map[string][]byte) {
			f["values.yaml"] = append(f["values.yaml"], []byte("\n---\nenabled: true\n")...)
		},
		"explicit end": func(f map[string][]byte) {
			f["Chart.yaml"] = append(f["Chart.yaml"], []byte("\n...\n---\nname: hidden\n")...)
		},
		"duplicate field": func(f map[string][]byte) {
			f["values.yaml"] = bytes.Replace(f["values.yaml"], []byte(`"enabled":false`), []byte(`"enabled":false,"enabled":true`), 1)
		},
		"hidden chart": func(f map[string][]byte) { f["charts/hidden/Chart.yaml"] = []byte("name: hidden") },
		"no templates": func(f map[string][]byte) {
			for name := range f {
				if strings.HasPrefix(name, "templates/") {
					delete(f, name)
				}
			}
		},
		"remote schema": func(f map[string][]byte) {
			updateYAML(t, f, "values.schema.json", func(v map[string]any) { v["$ref"] = "https://example.invalid/schema" })
		},
		"weakened schema": func(f map[string][]byte) {
			updateYAML(t, f, "values.schema.json", func(v map[string]any) { v["additionalProperties"] = true })
		},
		"missing required": func(f map[string][]byte) {
			updateYAML(t, f, "values.schema.json", func(v map[string]any) { v["required"] = []string{"enabled"} })
		},
		"dependency": func(f map[string][]byte) {
			updateYAML(t, f, "Chart.yaml", func(v map[string]any) { v["dependencies"] = []any{map[string]any{"name": "hidden"}} })
		},
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			raw, identity := chartFixture(t, change)
			if _, err := InspectChart(bytes.NewReader(raw), int64(len(raw)), sum(raw), identity); !errors.Is(err, ErrArchive) {
				t.Fatal("ambiguous chart accepted")
			}
		})
	}
}
func TestYAMLDecoderRejectsTrailingDocumentsAndDuplicateMapping(t *testing.T) {
	for _, data := range []string{"enabled: false\nenabled: true\n", "enabled: false\n---\nenabled: true\n", "enabled: false\n...\n---\n", "a: *missing\n"} {
		var value map[string]any
		if decodeYAML([]byte(data), &value) == nil {
			t.Fatal("ambiguous YAML accepted")
		}
	}
}
