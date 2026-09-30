package incident

import (
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/changemarkers"
)

const HistorySchemaVersion = api.HistoryIncidentSchemaVersion
const MaxHistoryBytes = api.MaxHistoryIncidentBytes

var historyCaveats = api.HistoryIncidentCaveats()

const historyAliasCaveat = api.HistoryIncidentAliasCaveat

type HistoryBundle = api.HistoryIncident

func NewHistory(c changemarkers.Context, version string, at time.Time, sensitive bool) (HistoryBundle, error) {
	b := HistoryBundle{SchemaVersion: HistorySchemaVersion, CapturedAt: at.UTC(), ToolVersion: version, Context: c, Caveats: append([]string(nil), historyCaveats...)}
	if err := ValidateHistory(b); err != nil {
		return HistoryBundle{}, err
	}
	encoded, err := json.Marshal(b)
	if err != nil {
		return HistoryBundle{}, err
	}
	var copied HistoryBundle
	if err := json.Unmarshal(encoded, &copied); err != nil {
		return HistoryBundle{}, err
	}
	if !sensitive {
		if err := redactHistory(&copied); err != nil {
			return HistoryBundle{}, err
		}
		copied.Caveats = append(copied.Caveats, historyAliasCaveat)
	}
	return copied, ValidateHistory(copied)
}

func ValidateHistory(b HistoryBundle) error {
	if err := api.ValidateHistoryIncident(b); err != nil {
		return err
	}
	if b.Redacted {
		return validateHistoryAliases(b.Context)
	}
	return nil
}

func WriteHistory(w io.Writer, path string, overwrite bool, b HistoryBundle) error {
	if err := ValidateHistory(b); err != nil {
		return err
	}
	encoder := json.NewEncoder(&boundedWriter{destination: io.Discard, remaining: MaxHistoryBytes})
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(b); err != nil {
		return fmt.Errorf("memory history incident exceeds its file byte limit")
	}
	return writeDocument(w, path, overwrite, b)
}
