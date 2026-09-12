package incident

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/capability"
	"github.com/danushkastanley/kube-memlens/internal/observation"
)

const RestrictedSchemaVersion = 3

type RestrictedBundle struct {
	SchemaVersion int               `json:"schemaVersion"`
	CapturedAt    time.Time         `json:"capturedAt"`
	ToolVersion   string            `json:"toolVersion"`
	Redacted      bool              `json:"redacted"`
	Observations  observation.Batch `json:"observations"`
}

// Document keeps incompatible memory domains separate at the decoding boundary.
type Document struct {
	Deep       *api.IncidentBundle
	Restricted *RestrictedBundle
	Node       *NodeBundle
}

func Read(path string) (Document, error) {
	file, err := os.Open(path)
	if err != nil {
		return Document{}, fmt.Errorf("open incident bundle: %w", err)
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, MaxBytes+1))
	if err != nil {
		return Document{}, fmt.Errorf("read incident bundle: %w", err)
	}
	if int64(len(data)) > MaxBytes {
		return Document{}, fmt.Errorf("incident bundle exceeds %d byte limit", MaxBytes)
	}
	var header struct {
		SchemaVersion int `json:"schemaVersion"`
	}
	if err := json.Unmarshal(data, &header); err != nil {
		return Document{}, fmt.Errorf("decode incident bundle: %w", err)
	}
	if header.SchemaVersion == NodeSchemaVersion {
		bundle, err := decodeNode(data)
		if err != nil {
			return Document{}, err
		}
		return Document{Node: &bundle}, nil
	}
	if header.SchemaVersion == RestrictedSchemaVersion {
		var bundle RestrictedBundle
		if err := decodeStrict(data, &bundle); err != nil {
			return Document{}, err
		}
		if err := ValidateRestricted(bundle); err != nil {
			return Document{}, err
		}
		return Document{Restricted: &bundle}, nil
	}
	var bundle api.IncidentBundle
	if err := decodeStrict(data, &bundle); err != nil {
		return Document{}, err
	}
	if err := ValidateSchema(bundle); err != nil {
		return Document{}, err
	}
	if len(bundle.Pods) > 10_000 || len(bundle.Nodes) > 10_000 || len(bundle.Histories) > 10_000 {
		return Document{}, fmt.Errorf("incident bundle exceeds entity limits")
	}
	return Document{Deep: &bundle}, nil
}

func decodeStrict(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode incident bundle: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return fmt.Errorf("decode incident bundle: unexpected trailing JSON")
	}
	return nil
}

func WriteRestricted(stdout io.Writer, output string, overwrite bool, bundle RestrictedBundle) error {
	if err := ValidateRestricted(bundle); err != nil {
		return err
	}
	return writeDocument(stdout, output, overwrite, bundle)
}

// NewRestricted copies the data before redacting so live frames retain identity.
func NewRestricted(batch observation.Batch, namespace, podName, version string, at time.Time, sensitive bool) (RestrictedBundle, error) {
	if batch.Mode != capability.Restricted {
		return RestrictedBundle{}, fmt.Errorf("restricted capture requires restricted observations")
	}
	data, err := json.Marshal(batch)
	if err != nil {
		return RestrictedBundle{}, err
	}
	var copy observation.Batch
	if err := json.Unmarshal(data, &copy); err != nil {
		return RestrictedBundle{}, err
	}
	selected := make([]observation.Pod, 0, len(copy.Pods))
	for _, pod := range copy.Pods {
		if (namespace != "" && pod.Namespace != namespace) || (podName != "" && pod.Name != podName) {
			continue
		}
		if !sensitive {
			pod.UID, pod.Context.Labels = "", nil
			for i := range pod.Containers {
				pod.Containers[i].Context.Labels = nil
			}
		}
		selected = append(selected, pod)
	}
	if podName != "" && len(selected) == 0 {
		return RestrictedBundle{}, fmt.Errorf("selected Pod is not present in current observations")
	}
	copy.Pods = selected
	if namespace != "" || podName != "" {
		copy.Nodes = nil
		copy.Completeness = capability.Partial
		copy.Caveats = append(copy.Caveats, "Node observations are omitted from a scoped capture; groups cover only captured Pods.")
	}
	if !sensitive {
		for i := range copy.Nodes {
			copy.Nodes[i].UID = ""
		}
	}
	copy.Namespaces, copy.Workloads = observation.GroupPods(copy.Pods)
	partialGroups := podName != ""
	for _, source := range copy.Sources {
		if source.Source == capability.KubernetesStatus && source.Scope == capability.PodScope && source.Completeness != capability.Complete {
			partialGroups = true
		}
	}
	if partialGroups {
		for i := range copy.Namespaces {
			copy.Namespaces[i].WorkingSet.Evidence.Completeness = capability.Partial
		}
		for i := range copy.Workloads {
			copy.Workloads[i].WorkingSet.Evidence.Completeness = capability.Partial
		}
	}
	result := RestrictedBundle{SchemaVersion: RestrictedSchemaVersion, CapturedAt: at.UTC(), ToolVersion: version, Redacted: !sensitive, Observations: copy}
	return result, ValidateRestricted(result)
}
