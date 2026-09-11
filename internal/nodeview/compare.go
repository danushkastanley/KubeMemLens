package nodeview

import (
	"fmt"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/model"
)

// ComparisonLines expects identity/boot compatibility checked by the caller.
// Rows are independent measurements, with missing operands left unreported.
func ComparisonLines(before, after api.NodeEvidence, width int) []string {
	b, a := before.Analysis, after.Analysis
	lines := []string{"Node comparison: " + a.NodeName, "Source samples: " + instant(b.Facts.Memory.CapturedAt) + " / " + instant(a.Facts.Memory.CapturedAt), "Independent measurements; rows are not a partition.", "SIGNAL | BEFORE | AFTER | DELTA"}
	for _, row := range []struct {
		name          string
		before, after *uint64
	}{
		{"Usage", b.Facts.Memory.UsageBytes, a.Facts.Memory.UsageBytes}, {"Available", b.Facts.Memory.AvailableBytes, a.Facts.Memory.AvailableBytes},
		{"Working set", b.Facts.Memory.WorkingSetBytes, a.Facts.Memory.WorkingSetBytes}, {"RSS", b.Facts.Memory.RSSBytes, a.Facts.Memory.RSSBytes},
		{"Observed Pod charge", b.ObservedPodCharge, a.ObservedPodCharge},
	} {
		lines = append(lines, comparisonRow(row.name, row.before, row.after))
	}
	if b.Facts.Swap != nil && a.Facts.Swap != nil {
		lines = append(lines, comparisonRow("Swap allocation", b.Facts.Swap.UsageBytes, a.Facts.Swap.UsageBytes), "Swap samples: "+instant(b.Facts.Swap.CapturedAt)+" / "+instant(a.Facts.Swap.CapturedAt))
	}
	if b.OutsidePods.Bytes != nil && a.OutsidePods.Bytes != nil && b.OutsidePods.Qualification == a.OutsidePods.Qualification {
		lines = append(lines, comparisonRow("Outside observed Pods estimate", b.OutsidePods.Bytes, a.OutsidePods.Bytes))
	} else {
		lines = append(lines, "Outside observed Pods estimates: unavailable or different qualifications")
	}
	if b.Unaccounted.Bytes != nil && a.Unaccounted.Bytes != nil && b.Unaccounted.Qualification == a.Unaccounted.Qualification {
		lines = append(lines, comparisonRow("Unaccounted estimate", b.Unaccounted.Bytes, a.Unaccounted.Bytes))
	} else {
		lines = append(lines, "Unaccounted estimates: unavailable or different qualifications")
	}
	lines = append(lines, fmt.Sprintf("Severity: %s -> %s; confidence: %s -> %s", b.Severity, a.Severity, b.Confidence, a.Confidence), "Contributor identities are not matched across captures.")
	return Wrap(lines, width)
}

func comparisonRow(name string, before, after *uint64) string {
	delta := "unreported"
	if before != nil && after != nil {
		if *after >= *before {
			delta = "+" + model.FormatBytes(*after-*before)
		} else {
			delta = "-" + model.FormatBytes(*before-*after)
		}
	}
	return name + " | " + bytes(before) + " | " + bytes(after) + " | " + delta
}
