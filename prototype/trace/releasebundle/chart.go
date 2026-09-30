package releasebundle

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"reflect"
	"strings"

	json "encoding/json/v2"
	yaml "go.yaml.in/yaml/v2"
	sigsyaml "sigs.k8s.io/yaml"
)

const chartRoot = "kube-memlens-trace/"
const imageRepository = "ghcr.io/danushkastanley/kube-memlens-trace"

type Chart struct {
	files          FileSet
	version, image string
}

func (Chart) Format(w fmt.State, _ rune) { _, _ = io.WriteString(w, "[release chart inventory]") }
func (c Chart) Version() string          { return c.version }
func (c Chart) ImageReference() string   { return c.image }

type chartMetadata struct {
	APIVersion  string   `json:"apiVersion"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Type        string   `json:"type"`
	Version     string   `json:"version"`
	AppVersion  string   `json:"appVersion"`
	Home        string   `json:"home"`
	Sources     []string `json:"sources"`
}

// InspectChart checks package metadata and frozen disabled defaults. The consumer
// must also lint/render the package and validate the resulting Kubernetes objects.
// No template is evaluated, file extracted or cluster contacted here.
func InspectChart(source io.ReaderAt, size int64, archiveSHA256 string, identity ImageIdentity) (Chart, error) {
	if source == nil || size <= 0 || size > 8<<20 || !shaPattern.MatchString(archiveSHA256) || !identity.valid() {
		return Chart{}, ErrArchive
	}
	keep := map[string]int64{chartRoot + "Chart.yaml": 65536, chartRoot + "values.yaml": 65536, chartRoot + "values.schema.json": 65536}
	inventory, err := ReadArchive(io.NewSectionReader(source, 0, size), TarGzip, Limits{8 << 20, 16 << 20, 256 << 10, 128}, keep)
	if err != nil || inventory.InputDigest() != archiveSHA256 || inventory.InputBytes() != size {
		return Chart{}, ErrArchive
	}
	templates := 0
	for _, name := range inventory.Names() {
		entry, _ := inventory.Entry(name)
		if entry.Type == Directory {
			if name != "kube-memlens-trace" && name != "kube-memlens-trace/templates" {
				return Chart{}, ErrArchive
			}
			continue
		}
		if entry.Mode&0111 != 0 {
			return Chart{}, ErrArchive
		}
		relative := strings.TrimPrefix(name, chartRoot)
		if name == relative {
			return Chart{}, ErrArchive
		}
		switch relative {
		case "Chart.yaml", "values.yaml", "values.schema.json", "README.md":
		default:
			if !strings.HasPrefix(relative, "templates/") || strings.Count(relative, "/") != 1 || (!strings.HasSuffix(relative, ".yaml") && !strings.HasSuffix(relative, ".tpl")) {
				return Chart{}, ErrArchive
			}
			templates++
		}
	}
	if templates == 0 {
		return Chart{}, ErrArchive
	}
	read := func(name string) []byte {
		entry, _ := inventory.Entry(chartRoot + name)
		data, _ := entry.Bytes()
		return data
	}
	var metadata chartMetadata
	if decodeYAML(read("Chart.yaml"), &metadata) != nil || metadata.APIVersion != "v2" || metadata.Name != "kube-memlens-trace" || metadata.Type != "application" || metadata.Version != identity.Version || metadata.AppVersion != identity.Version || metadata.Description == "" || !safeMetadata(metadata.Description) || metadata.Home != "https://github.com/danushkastanley/KubeMemLens" || !reflect.DeepEqual(metadata.Sources, []string{metadata.Home}) {
		return Chart{}, ErrArchive
	}
	var values map[string]any
	if decodeYAML(read("values.yaml"), &values) != nil || !reflect.DeepEqual(values, chartDefaults(identity.IndexDigest)) {
		return Chart{}, ErrArchive
	}
	if validateChartSchema(read("values.schema.json"), values) != nil {
		return Chart{}, ErrArchive
	}
	return Chart{files: inventory.files, version: identity.Version, image: imageRepository + "@" + identity.IndexDigest}, nil
}
func chartDefaults(digest string) map[string]any {
	return map[string]any{
		"enabled": false, "profile": "", "acknowledgeUnqualifiedDevelopment": false,
		"image":                     map[string]any{"repository": imageRepository, "digest": digest, "pullPolicy": "IfNotPresent"},
		"acceptancePolicyConfigMap": "", "acceptancePolicySHA256": "", "apiTLSSecret": "",
		"auditReferenceKeySecret": "", "auditReferenceKeySHA256": "", "apiCABundle": "",
		"controlCertificateSHA256": "", "preflightServiceAccount": "", "nodes": []any{},
		"allowConfirmedPaths": false, "maxNodeTraces": float64(1),
	}
}
func decodeYAML(data []byte, value any) error {
	if len(data) == 0 || len(data) > 65536 {
		return ErrArchive
	}
	decoder := yaml.NewDecoder(bufio.NewReader(bytes.NewReader(data)))
	decoder.SetStrict(true)
	var document, extra any
	if decoder.Decode(&document) != nil || decoder.Decode(&extra) != io.EOF {
		return ErrArchive
	}
	encoded, err := sigsyaml.YAMLToJSONStrict(data)
	if err != nil || len(encoded) > 256<<10 || json.Unmarshal(encoded, value, json.RejectUnknownMembers(true)) != nil {
		return ErrArchive
	}
	return nil
}

func (c Chart) Files() FileSet { return c.files }
