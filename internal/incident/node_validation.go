package incident

import (
	"encoding/json"
	"fmt"
	"reflect"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/capability"
	"github.com/danushkastanley/kube-memlens/internal/nodecontext"
)

func ValidateNode(b NodeBundle) error {
	e, a, r := b.Evidence, b.Evidence.Analysis, b.Evidence.Record
	if b.SchemaVersion != NodeSchemaVersion || b.CapturedAt.IsZero() || b.ToolVersion == "" || len(b.ToolVersion) > 512 || a.EvaluatedAt.IsZero() || a.EvaluatedAt.After(b.CapturedAt) || !e.Consistent() || !dns(r.NodeName) || len(r.NodeUID) > 128 {
		return fmt.Errorf("invalid Node incident schema, identity or capture time")
	}
	if err := validateScalars(reflect.ValueOf(b)); err != nil {
		return err
	}
	if !oneOf(string(r.Freshness), "fresh", "stale", "rebuilding", "unavailable") || r.ReceivedAt.After(b.CapturedAt.Add(30*time.Second)) {
		return fmt.Errorf("invalid Node record freshness or receive time")
	}
	if r.Report == nil && (r.LastGood != nil || !r.ReceivedAt.IsZero()) || r.Report != nil && r.ReceivedAt.IsZero() {
		return fmt.Errorf("Node record source and receive time disagree")
	}
	if b.Redacted && !nodeFingerprintPattern.MatchString(r.NodeUID) {
		return fmt.Errorf("redacted Node incident contains raw identity")
	}
	for _, o := range []*nodecontext.Observation{r.Report, r.LastGood} {
		if o == nil {
			continue
		}
		if err := validateNodeObservation(*o, r.NodeName, r.NodeUID, b.CapturedAt); err != nil {
			return err
		}
	}
	if r.LastGood != nil && r.LastGood.Availability != capability.Available {
		return fmt.Errorf("last-good Node observation is not available")
	}
	if err := validateNodeAnalysis(a, b.Redacted); err != nil {
		return err
	}
	if b.History != nil {
		if err := validateNodeHistory(*b.History, r.NodeName, b.CapturedAt, b.Redacted); err != nil {
			return err
		}
	}
	body, err := json.Marshal(b)
	if err != nil || len(body) > MaxNodeBytes {
		return fmt.Errorf("Node incident exceeds its encoded byte limit")
	}
	return nil
}

func validateNodeObservation(o nodecontext.Observation, name, uid string, captured time.Time) error {
	if o.NodeName != name || o.NodeUID != uid || o.ReportedAt.After(captured.Add(30*time.Second)) {
		return fmt.Errorf("Node incident observation identity or time mismatch")
	}
	// Validate relative to the original report, not replay wall-clock time.
	if err := nodecontext.Validate(o, o.ReportedAt, 24*time.Hour, 30*time.Second); err != nil {
		return fmt.Errorf("invalid Node incident observation: %w", err)
	}
	return nil
}

func validateNodeHistory(h api.NodeContextHistory, name string, captured time.Time, redacted bool) error {
	if h.NodeName != name || h.Generation == "" || len(h.Generation) > 128 || h.Continue != "" || h.ResetAt.IsZero() || h.ResetAt.After(captured) ||
		h.WindowSeconds < 0 || h.WindowSeconds > int64(nodecontext.HistoryDuration.Seconds()) || !complete(h.Completeness) || len(h.Series) > MaxNodeInstances {
		return fmt.Errorf("invalid Node incident history metadata or bounds")
	}
	if redacted && !nodeFingerprintPattern.MatchString(h.Generation) {
		return fmt.Errorf("redacted Node history contains raw generation")
	}
	if h.Completeness == capability.Complete && (h.CoverageLost || len(h.Series) != 1) {
		return fmt.Errorf("partial Node history cannot claim complete coverage")
	}
	seen := map[string]bool{}
	for _, s := range h.Series {
		if s.NodeUID == "" || len(s.NodeUID) > 128 || seen[s.NodeUID] || len(s.Points) > nodecontext.MaxHistoryPoints || redacted && !nodeFingerprintPattern.MatchString(s.NodeUID) {
			return fmt.Errorf("invalid Node history instance")
		}
		seen[s.NodeUID] = true
		previous := time.Time{}
		for _, p := range s.Points {
			if p.ReceivedAt.IsZero() || p.ReceivedAt.After(captured) || !previous.IsZero() && !p.ReceivedAt.After(previous) {
				return fmt.Errorf("Node history receive order is invalid")
			}
			if err := validateNodeObservation(p.Observation, name, s.NodeUID, captured); err != nil {
				return err
			}
			if p.Observation.Availability != capability.Available {
				return fmt.Errorf("Node history point lacks source measurements")
			}
			previous = p.ReceivedAt
		}
	}
	return nil
}
