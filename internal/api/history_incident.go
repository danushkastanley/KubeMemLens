package api

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/changemarkers"
)

const HistoryIncidentSchemaVersion = 6
const MaxHistoryIncidentBytes = 2 << 20
const HistoryIncidentAliasCaveat = "Aliases apply only within this capture; they cannot establish identity continuity across captures."

func HistoryIncidentCaveats() []string {
	return []string{
		"Retained events are best-effort reports; missing events do not prove no changes.",
		"Source clocks may differ; correlation does not establish causation.",
		"History covers current selected instances; markers do not join memory across UIDs.",
	}
}

type HistoryIncident struct {
	SchemaVersion int                   `json:"schemaVersion"`
	CapturedAt    time.Time             `json:"capturedAt"`
	ToolVersion   string                `json:"toolVersion"`
	Redacted      bool                  `json:"redacted"`
	Context       changemarkers.Context `json:"context"`
	Caveats       []string              `json:"caveats"`
}

// ValidateHistoryIncident checks the shared wire contract. Redacted capture
// writers/readers must additionally validate their identity-alias projection.
func ValidateHistoryIncident(b HistoryIncident) error {
	if b.SchemaVersion != HistoryIncidentSchemaVersion || b.Context.Validate() != nil || b.CapturedAt.IsZero() || b.Context.Changes.ObservedAt.After(b.CapturedAt) || b.Context.History.ReceivedAt.After(b.CapturedAt) || b.ToolVersion == "" || len(b.ToolVersion) > 512 || strings.IndexFunc(b.ToolVersion, func(r rune) bool { return r < 32 || r == 127 }) >= 0 {
		return fmt.Errorf("invalid memory history incident")
	}
	expected := HistoryIncidentCaveats()
	if b.Redacted {
		expected = append(expected, HistoryIncidentAliasCaveat)
	}
	if !slices.Equal(expected, b.Caveats) {
		return fmt.Errorf("memory history incident lacks its provenance caveats")
	}
	encoded, err := json.Marshal(b)
	if err != nil || len(encoded) > MaxHistoryIncidentBytes {
		return fmt.Errorf("memory history incident exceeds its byte limit")
	}
	return nil
}
