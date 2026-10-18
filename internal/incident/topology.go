package incident

import (
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/memorytopology"
)

const TopologySchemaVersion = 7

// TopologyBundle explicitly wraps the unchanged schema-4 Node document. It is
// never added to an ordinary capture merely because the server supports it.
type TopologyBundle struct {
	SchemaVersion int                    `json:"schemaVersion"`
	CapturedAt    time.Time              `json:"capturedAt"`
	Redacted      bool                   `json:"redacted"`
	Node          NodeBundle             `json:"node"`
	Current       *memorytopology.Report `json:"current,omitempty"`
	LastGood      *memorytopology.Report `json:"lastGood,omitempty"`
	Caveats       []string               `json:"caveats"`
}

const topologyAliasCaveat = "NUMA domain aliases apply only within this capture and cannot establish topology continuity across captures."

func NewTopology(node NodeBundle, topology api.NodeMemoryTopology, sensitive bool) (TopologyBundle, error) {
	if node.Redacted || topology.Validate(node.Evidence.Record.NodeName) != nil {
		return TopologyBundle{}, memorytopology.ErrInvalid
	}
	b := TopologyBundle{SchemaVersion: TopologySchemaVersion, CapturedAt: node.CapturedAt, Node: node, Current: topology.Current, LastGood: topology.LastGood, Caveats: []string{}}
	if err := ValidateTopology(b); err != nil {
		return TopologyBundle{}, err
	}
	raw, err := json.Marshal(b)
	if err != nil {
		return TopologyBundle{}, err
	}
	var copy TopologyBundle
	if err := json.Unmarshal(raw, &copy); err != nil {
		return TopologyBundle{}, err
	}
	if !sensitive {
		if err := redactTopology(&copy); err != nil {
			return TopologyBundle{}, err
		}
	}
	return copy, ValidateTopology(copy)
}
func ValidateTopology(b TopologyBundle) error {
	if b.SchemaVersion != TopologySchemaVersion || !b.CapturedAt.Equal(b.Node.CapturedAt) || b.Redacted != b.Node.Redacted {
		return memorytopology.ErrInvalid
	}
	if err := ValidateNode(b.Node); err != nil {
		return err
	}
	for _, r := range []*memorytopology.Report{b.Current, b.LastGood} {
		if r == nil {
			continue
		}
		if err := r.Validate(); err != nil {
			return err
		}
		if r.ObservedAt.After(b.CapturedAt) || r.Observation.NodeName != b.Node.Evidence.Record.NodeName || r.Observation.NodeUID != b.Node.Evidence.Record.NodeUID {
			return memorytopology.ErrInvalid
		}
	}
	if b.Current == nil && b.LastGood != nil {
		return memorytopology.ErrInvalid
	}
	if b.Redacted {
		if len(b.Caveats) != 1 || b.Caveats[0] != topologyAliasCaveat || b.Node.Evidence.Record.NodeName != "node-1" {
			return memorytopology.ErrInvalid
		}
		ids := topologyIDs(b)
		for i, id := range ids {
			if id != uint16(i) {
				return memorytopology.ErrInvalid
			}
		}
	} else if len(b.Caveats) != 0 {
		return memorytopology.ErrInvalid
	}
	raw, err := json.Marshal(b)
	if err != nil || len(raw) > MaxNodeBytes {
		return memorytopology.ErrBounds
	}
	return nil
}
func WriteTopology(w io.Writer, path string, overwrite bool, b TopologyBundle) error {
	if err := ValidateTopology(b); err != nil {
		return err
	}
	check := json.NewEncoder(&boundedWriter{destination: io.Discard, remaining: MaxNodeBytes})
	check.SetIndent("", "  ")
	if err := check.Encode(b); err != nil {
		return fmt.Errorf("topology incident exceeds its file limit: %w", err)
	}
	return writeDocument(w, path, overwrite, b)
}
