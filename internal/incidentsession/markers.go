package incidentsession

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	json "encoding/json/v2"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/memoryhistory"
)

func (s *Store) RecordMarkers(ctx context.Context, p Principal, id string, data []byte) (Summary, error) {
	if err := s.authorize(ctx, p, Markers, id); err != nil {
		return Summary{}, err
	}
	evidence, err := historyCapture(p, data)
	if err != nil {
		return Summary{}, err
	}
	var bundle api.HistoryIncident
	if json.Unmarshal(data, &bundle) != nil {
		return Summary{}, ErrInvalid
	}
	return s.retainEvidence(ctx, p, id, evidence, Marked, "kubernetes", markerGaps(bundle)...)
}

func (s *Store) RecordMarkerGap(ctx context.Context, p Principal, id, reason string) (Summary, error) {
	if err := s.authorize(ctx, p, Markers, id); err != nil {
		return Summary{}, err
	}
	input := Input{Kind: Gap, Source: "kubernetes", GapReason: reason}
	if input.validate(p) != nil {
		return Summary{}, ErrInvalid
	}
	return s.mutate(ctx, p, id, input)
}

func decodeRetainedCapture(p Principal, data []byte) (CapturedEvidence, error) {
	var header struct {
		SchemaVersion int `json:"schemaVersion"`
	}
	if len(data) == 0 || len(data) > MaxCaptureBytes || json.Unmarshal(data, &header) != nil {
		return CapturedEvidence{}, ErrInvalid
	}
	if header.SchemaVersion == api.HistoryIncidentSchemaVersion {
		return historyCapture(p, data)
	}
	return podCapture(p, data)
}

func historyCapture(p Principal, data []byte) (CapturedEvidence, error) {
	if len(data) == 0 || len(data) > MaxCaptureBytes || boundedCaptureData(data) != nil || !explicitPrivateCapture(data) {
		return CapturedEvidence{}, ErrInvalid
	}
	var bundle api.HistoryIncident
	if json.Unmarshal(data, &bundle, json.RejectUnknownMembers(true), json.MatchCaseInsensitiveNames(false)) != nil || api.ValidateHistoryIncident(bundle) != nil || bundle.Redacted {
		return CapturedEvidence{}, ErrInvalid
	}
	request := bundle.Context.History.Selection.Request
	if request.Scope != memoryhistory.Pod || request.Namespace != p.Namespace {
		return CapturedEvidence{}, ErrInvalid
	}
	sum := sha256.Sum256(data)
	return CapturedEvidence{NamespaceUID: p.NamespaceUID, Digest: hex.EncodeToString(sum[:]), SchemaVersion: bundle.SchemaVersion, ObservedAt: bundle.CapturedAt.UTC(), Data: bytes.Clone(data)}, nil
}

func markerGaps(bundle api.HistoryIncident) []Input {
	var gaps []Input
	changes := bundle.Context.Changes
	gaps = append(gaps, Input{Kind: Gap, Source: "kubernetes", ObservedAt: changes.ObservedAt, GapReason: "events-" + string(changes.Events)})
	if changes.Truncated {
		gaps = append(gaps, Input{Kind: Gap, Source: "kubernetes", ObservedAt: changes.ObservedAt, GapReason: "events-truncated"})
	}
	history := bundle.Context.History
	source := "collector"
	if history.Query.Source == memoryhistory.Prometheus {
		source = "prometheus"
	}
	if history.State != memoryhistory.Fresh {
		gaps = append(gaps, Input{Kind: Gap, Source: source, ObservedAt: history.ReceivedAt, GapReason: "history-" + string(history.State)})
	} else if history.Completeness != memoryhistory.Complete {
		gaps = append(gaps, Input{Kind: Gap, Source: source, ObservedAt: history.ReceivedAt, GapReason: "partial-evidence"})
	}
	return gaps
}

func explicitPrivateCapture(data []byte) bool {
	var header struct {
		Redacted *bool `json:"redacted"`
	}
	return json.Unmarshal(data, &header) == nil && header.Redacted != nil && !*header.Redacted
}
