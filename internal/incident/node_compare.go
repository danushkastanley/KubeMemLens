package incident

import (
	"fmt"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/capability"
	"github.com/danushkastanley/kube-memlens/internal/nodecontext"
)

func CompatibleNodes(before, after NodeBundle) error {
	for _, b := range []NodeBundle{before, after} {
		if err := ValidateNode(b); err != nil {
			return err
		}
		a := b.Evidence.Analysis
		if b.Evidence.Record.LastGood == nil || b.Evidence.Record.LastGood.Stats == nil || a.Facts.Memory == nil || a.Facts.Availability != capability.Available ||
			a.EvaluatedAt.Sub(a.Facts.Memory.CapturedAt) > nodecontext.StaleAfter || a.Facts.Memory.CapturedAt.After(a.EvaluatedAt.Add(30*time.Second)) {
			return fmt.Errorf("Node comparison requires available, fresh memory samples and a known boot")
		}
	}
	b, a := before.Evidence, after.Evidence
	if b.Record.NodeName != a.Record.NodeName || nodeFingerprint(b.Record.NodeUID) != nodeFingerprint(a.Record.NodeUID) ||
		!b.Record.LastGood.Stats.StartedAt.Equal(a.Record.LastGood.Stats.StartedAt) || b.Record.LastGood.Stats.Provenance != a.Record.LastGood.Stats.Provenance {
		return fmt.Errorf("Node comparison requires the same Node instance, boot and source provenance")
	}
	if after.CapturedAt.Before(before.CapturedAt) || a.Analysis.Facts.Memory.CapturedAt.Before(b.Analysis.Facts.Memory.CapturedAt) {
		return fmt.Errorf("Node comparison timestamps are reversed")
	}
	return nil
}
