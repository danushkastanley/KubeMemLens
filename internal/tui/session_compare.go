package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"unicode"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/client"
	"github.com/danushkastanley/kube-memlens/internal/historyview"
	"github.com/danushkastanley/kube-memlens/internal/incident"
	"github.com/danushkastanley/kube-memlens/internal/incidentsession"
)

func runSessionComparison(ctx context.Context, connection client.IncidentSessions, request sessionRequest) sessionPanelMsg {
	result := sessionPanelMsg{operation: request.operation}
	selectors := strings.Fields(request.input)
	if len(selectors) != 2 {
		result.err = incidentsession.ErrInvalid
		return result
	}
	data, err := connection.ExportIncident(ctx, request.id, incidentsession.ExportAuthorised)
	if err != nil {
		result.err = err
		return result
	}
	doc, err := incidentsession.DecodeExport(data)
	if err != nil || doc.Authorised == nil {
		result.err = incidentsession.ErrInvalid
		return result
	}
	before, err := incidentsession.SelectCapture(*doc.Authorised, selectors[0])
	if err != nil {
		result.err = err
		return result
	}
	after, err := incidentsession.SelectCapture(*doc.Authorised, selectors[1])
	if err != nil {
		result.err = err
		return result
	}
	lines, err := sessionComparisonLines(before, after)
	if err != nil {
		result.err = err
		return result
	}
	result.summary, result.err = connection.CompareIncident(ctx, request.id, before.Digest, after.Digest)
	if result.err == nil {
		result.lines = lines
	}
	return result
}

func sessionComparisonLines(before, after incidentsession.CapturedEvidence) ([]string, error) {
	lines := []string{"Retained evidence comparison"}
	if after.ObservedAt.Before(before.ObservedAt) {
		lines = append(lines, "Capture clocks are not ordered; no change rate is inferred.")
	}
	if before.SchemaVersion == 6 && after.SchemaVersion == 6 {
		var a, b incident.HistoryBundle
		if json.Unmarshal(before.Data, &a) != nil || json.Unmarshal(after.Data, &b) != nil {
			return nil, incidentsession.ErrInvalid
		}
		if !b.CapturedAt.Before(a.CapturedAt) && !b.Context.Changes.ObservedAt.Before(a.Context.Changes.ObservedAt) {
			comparison, err := incident.CompareHistory(a, b)
			if err != nil {
				return nil, err
			}
			lines = append(lines, "Identity continuity: "+comparison.Continuity)
			if !comparison.ComparableMetric {
				lines = append(lines, "Sources or metrics differ; values are not directly comparable.")
			}
		}
		lines = append(lines, "Before:")
		lines = append(lines, historyview.ContextLines(a.Context, a.CapturedAt, 100)...)
		lines = append(lines, "After:")
		lines = append(lines, historyview.ContextLines(b.Context, b.CapturedAt, 100)...)
	} else {
		if before.SchemaVersion < 1 || before.SchemaVersion > 2 || after.SchemaVersion < 1 || after.SchemaVersion > 2 {
			return nil, fmt.Errorf("comparison requires captures from the same memory domain")
		}
		var a, b api.IncidentBundle
		if json.Unmarshal(before.Data, &a) != nil || json.Unmarshal(after.Data, &b) != nil || len(a.Pods) != 1 || len(b.Pods) != 1 {
			return nil, incidentsession.ErrInvalid
		}
		left, right := a.Pods[0], b.Pods[0]
		if left.PodUID != right.PodUID || left.PodName != right.PodName || left.NodeName != right.NodeName {
			lines = append(lines, "Different Pod instances or selections; values describe separate observations.")
		}
		for i := range left.Containers {
			left.Containers[i].ContainerName = strconv.Quote(left.Containers[i].ContainerName)
		}
		for i := range right.Containers {
			right.Containers[i].ContainerName = strconv.Quote(right.Containers[i].ContainerName)
		}
		comparison, err := compareResult(actionRequest{before: &left, after: &right})
		if err != nil {
			return nil, err
		}
		lines = append(lines, comparison.lines...)
	}
	total := 0
	for i, line := range lines {
		var safe strings.Builder
		for _, r := range line {
			if unicode.IsControl(r) || unicode.In(r, unicode.Cf, unicode.Zl, unicode.Zp) {
				quoted := strconv.QuoteRune(r)
				safe.WriteString(quoted[1 : len(quoted)-1])
			} else {
				safe.WriteRune(r)
			}
		}
		lines[i] = safe.String()
		total += len(lines[i])
	}
	if total > 128<<10 {
		return nil, incidentsession.ErrCapacity
	}
	return lines, nil
}
