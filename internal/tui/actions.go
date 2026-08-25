package tui

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"runtime"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/buildinfo"
	"github.com/danushkastanley/kube-memlens/internal/explain"
	"github.com/danushkastanley/kube-memlens/internal/incident"
	"github.com/danushkastanley/kube-memlens/internal/model"
	"github.com/danushkastanley/kube-memlens/internal/recommend"
)

type actionKind int

const (
	actionRecommend actionKind = iota
	actionCompare
	actionCapture
)

type actionRequest struct {
	kind        actionKind
	ref         entityRef
	pods        []api.PodSnapshot
	nodes       []api.NodeSnapshotStatus
	histories   []api.PodHistory
	before      *api.PodSnapshot
	after       *api.PodSnapshot
	outputPath  string
	overwrite   bool
	partial     bool
	caveats     []string
	reliability *api.CollectorReliability
}

type actionResult struct {
	title             string
	lines             []string
	outputPath        string
	overwriteRequired bool
}

type actionExecutor interface {
	Run(context.Context, actionRequest) (actionResult, error)
}

type localActionExecutor struct{}

func (localActionExecutor) Run(_ context.Context, request actionRequest) (actionResult, error) {
	switch request.kind {
	case actionRecommend:
		return recommendationResult(request)
	case actionCompare:
		return compareResult(request)
	case actionCapture:
		return captureResult(request)
	default:
		return actionResult{}, fmt.Errorf("unsupported incident action")
	}
}

func recommendationResult(request actionRequest) (actionResult, error) {
	finding, target, ok := findingForRef(request.ref, request.pods)
	if !ok {
		return actionResult{}, fmt.Errorf("selected entity is no longer available for recommendations")
	}
	items := recommend.ForFinding(finding)
	lines := []string{
		"Target: " + target,
		"Diagnosis: " + string(finding.Diagnosis),
		"Confidence: " + string(finding.Confidence),
		"Automatic mutation: disabled",
	}
	for _, item := range items {
		lines = append(lines, "", item.ID+" ["+item.Priority+"]", item.Action, "Why: "+item.Rationale)
		for _, condition := range item.Conditions {
			lines = append(lines, "- "+condition)
		}
	}
	return actionResult{title: "Read-only recommendations", lines: lines}, nil
}

func compareResult(request actionRequest) (actionResult, error) {
	if request.before == nil || request.after == nil {
		return actionResult{}, fmt.Errorf("comparison requires two Pods")
	}
	before, after := *request.before, *request.after
	lines := []string{
		"Before: " + before.Namespace + "/" + before.PodName,
		"After:  " + after.Namespace + "/" + after.PodName,
		"",
		fmt.Sprintf("%-16s %12s %12s %12s", "SIGNAL", "BEFORE", "AFTER", "DELTA"),
	}
	for _, row := range []struct {
		name          string
		before, after uint64
	}{
		{name: "Total", before: before.Memory.TotalBytes, after: after.Memory.TotalBytes},
		{name: "Anon", before: before.Memory.RSSBytes(), after: after.Memory.RSSBytes()},
		{name: "File cache", before: before.Memory.CacheBytes(), after: after.Memory.CacheBytes()},
		{name: "Shmem", before: before.Memory.ShmemBytes, after: after.Memory.ShmemBytes},
		{name: "Other", before: before.Memory.ResidualBytes(), after: after.Memory.ResidualBytes()},
		{name: "Kernel", before: before.Memory.KernelBytes, after: after.Memory.KernelBytes},
		{name: "Swap", before: before.Memory.SwapCurrentBytes, after: after.Memory.SwapCurrentBytes},
	} {
		lines = append(lines, fmt.Sprintf("%-16s %12s %12s %12s", row.name, model.FormatCompactBytes(row.before), model.FormatCompactBytes(row.after), signedTrend(row.before, row.after)))
	}
	beforeFinding, afterFinding := explain.AnalyzePod(before), explain.AnalyzePod(after)
	lines = append(lines,
		"", "Diagnosis: "+string(beforeFinding.Diagnosis)+" → "+string(afterFinding.Diagnosis),
		"Before observation: "+beforeFinding.EvidenceWindow.ObservationDescription(),
		"Before counters: "+beforeFinding.EvidenceWindow.DeltaDescription(),
		"After observation: "+afterFinding.EvidenceWindow.ObservationDescription(),
		"After counters: "+afterFinding.EvidenceWindow.DeltaDescription(),
	)
	return actionResult{title: "Live Pod comparison", lines: lines}, nil
}

func captureResult(request actionRequest) (actionResult, error) {
	if request.outputPath == "" {
		return actionResult{}, fmt.Errorf("capture path must not be empty")
	}
	pod, ok := podForRef(request.ref, request.pods)
	if !ok {
		return actionResult{}, fmt.Errorf("capture currently requires a selected Pod or container")
	}
	absolute, err := filepath.Abs(request.outputPath)
	if err != nil {
		return actionResult{}, fmt.Errorf("resolve capture path: %w", err)
	}
	bundle := api.IncidentBundle{
		SchemaVersion: api.CurrentIncidentSchemaVersion,
		CapturedAt:    time.Now().UTC(),
		ToolVersion:   buildinfo.Current(runtime.Version(), runtime.GOOS, runtime.GOARCH).String(),
		Redacted:      true,
		Partial:       request.partial,
		Reliability:   request.reliability,
		Pods:          []api.PodSnapshot{pod},
		Nodes:         captureNodes(pod, request.nodes, request.partial),
		Histories:     captureHistories(pod, request.histories),
	}
	if request.partial {
		bundle.Caveats = append(bundle.Caveats, request.caveats...)
	}
	incident.Redact(&bundle)
	err = incident.Write(io.Discard, absolute, request.overwrite, bundle)
	if err != nil {
		var exists incident.ExistsError
		if errors.As(err, &exists) {
			return actionResult{title: "Capture requires confirmation", outputPath: absolute, overwriteRequired: true}, err
		}
		return actionResult{}, err
	}
	return actionResult{
		title:      "Redacted capture written",
		outputPath: absolute,
		lines: []string{
			"Path: " + absolute,
			"Mode: 0600",
			"Pods: 1",
			fmt.Sprintf("History series: %d", len(bundle.Histories)),
			"Redacted: true",
			fmt.Sprintf("Partial: %t", bundle.Partial),
		},
	}, nil
}

func captureNodes(pod api.PodSnapshot, nodes []api.NodeSnapshotStatus, partial bool) []api.NodeSnapshotStatus {
	if partial || pod.NodeName == "" {
		return nil
	}
	for _, node := range nodes {
		if node.NodeName == pod.NodeName {
			return []api.NodeSnapshotStatus{node}
		}
	}
	return nil
}

func captureHistories(pod api.PodSnapshot, histories []api.PodHistory) []api.PodHistory {
	selected := make([]api.PodHistory, 0, len(histories))
	for _, history := range histories {
		if history.Namespace == pod.Namespace && history.PodName == pod.PodName &&
			(history.PodUID == "" || pod.PodUID == "" || history.PodUID == pod.PodUID) {
			selected = append(selected, history)
		}
	}
	return selected
}

func findingForRef(ref entityRef, pods []api.PodSnapshot) (explain.Result, string, bool) {
	if ref.kind == entityWorkload {
		selected := make([]api.PodSnapshot, 0)
		for _, pod := range pods {
			if pod.Namespace == ref.namespace && pod.Context.WorkloadKind == ref.workloadKind && pod.Context.WorkloadName == ref.name {
				selected = append(selected, pod)
			}
		}
		if len(selected) == 0 {
			return explain.Result{}, "", false
		}
		memories := make([]model.MemoryBreakdown, 0, len(selected))
		for _, pod := range selected {
			memories = append(memories, pod.Memory)
		}
		workload := api.WorkloadSnapshot{Namespace: ref.namespace, Kind: ref.workloadKind, Name: ref.name, Pods: selected, Memory: model.SumMemory(ref.name, memories)}
		return explain.AnalyzeWorkload(workload), ref.workloadKind + "/" + ref.namespace + "/" + ref.name, true
	}
	if ref.kind == entityContainer {
		for _, pod := range pods {
			if pod.Namespace != ref.namespace || pod.PodName != ref.podName {
				continue
			}
			for _, container := range pod.Containers {
				if container.ContainerName == ref.containerName {
					return explain.AnalyzeContainer(container), "Container/" + ref.namespace + "/" + ref.podName + "/" + ref.containerName, true
				}
			}
		}
		return explain.Result{}, "", false
	}
	pod, ok := podForRef(ref, pods)
	if !ok {
		return explain.Result{}, "", false
	}
	return explain.AnalyzePod(pod), "Pod/" + pod.Namespace + "/" + pod.PodName, true
}

func podForRef(ref entityRef, pods []api.PodSnapshot) (api.PodSnapshot, bool) {
	for _, pod := range pods {
		if pod.Namespace == ref.namespace && pod.PodName == ref.podName {
			return pod, true
		}
	}
	return api.PodSnapshot{}, false
}

func osc52Sequence(value string) string {
	return "\x1b]52;c;" + base64.StdEncoding.EncodeToString([]byte(value)) + "\a"
}
