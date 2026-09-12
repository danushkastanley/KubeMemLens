package incident

import (
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
)

const NodeSchemaVersion = 4
const MaxNodeInstances = 8
const MaxNodeBytes = 16 << 20

type NodeBundle struct {
	SchemaVersion int                     `json:"schemaVersion"`
	CapturedAt    time.Time               `json:"capturedAt"`
	ToolVersion   string                  `json:"toolVersion"`
	Redacted      bool                    `json:"redacted"`
	Evidence      api.NodeEvidence        `json:"evidence"`
	History       *api.NodeContextHistory `json:"history,omitempty"`
}

// NewNode copies the authorised read before redacting, preserving live state.
func NewNode(evidence api.NodeEvidence, history *api.NodeContextHistory, version string, at time.Time, sensitive bool) (NodeBundle, error) {
	b := NodeBundle{SchemaVersion: NodeSchemaVersion, CapturedAt: at.UTC(), ToolVersion: version, Evidence: evidence, History: history}
	if err := ValidateNode(b); err != nil {
		return NodeBundle{}, err
	}
	body, err := json.Marshal(b)
	if err != nil {
		return NodeBundle{}, err
	}
	var result NodeBundle
	if err := json.Unmarshal(body, &result); err != nil {
		return NodeBundle{}, err
	}
	if !sensitive {
		redactNode(&result)
	}
	return result, ValidateNode(result)
}

func WriteNode(stdout io.Writer, output string, overwrite bool, bundle NodeBundle) error {
	if err := ValidateNode(bundle); err != nil {
		return err
	}
	check := json.NewEncoder(&boundedWriter{destination: io.Discard, remaining: MaxNodeBytes})
	check.SetIndent("", "  ")
	if err := check.Encode(bundle); err != nil {
		return fmt.Errorf("Node incident exceeds its %d byte file limit: %w", MaxNodeBytes, err)
	}
	return writeDocument(stdout, output, overwrite, bundle)
}
