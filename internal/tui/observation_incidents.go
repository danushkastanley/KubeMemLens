package tui

import (
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/danushkastanley/kube-memlens/internal/buildinfo"
	"github.com/danushkastanley/kube-memlens/internal/incident"
	"github.com/danushkastanley/kube-memlens/internal/observationview"
)

func (m *appModel) captureObservationRef() (entityRef, bool) {
	if m.statusErr != nil || m.data.Observations == nil {
		m.setActionError(fmt.Errorf("capture and comparison require a current authorised observation read"))
		return entityRef{}, false
	}
	ref, ok := m.currentActionRef()
	if !ok || (ref.kind != entityPod && ref.kind != entityContainer) {
		m.setActionError(fmt.Errorf("select a Pod or container first"))
		return entityRef{}, false
	}
	return ref, true
}

func (m *appModel) startObservationCapture(overwrite bool) tea.Cmd {
	ref, ok := m.captureObservationRef()
	if !ok {
		return nil
	}
	version := buildinfo.Current(runtime.Version(), runtime.GOOS, runtime.GOARCH).String()
	bundle, err := incident.NewRestricted(*m.data.Observations, ref.namespace, ref.podName, version, time.Now(), false)
	if err != nil {
		m.setActionError(err)
		return nil
	}
	return m.startAction(actionRequest{kind: actionCapture, ref: ref, restricted: &bundle, outputPath: strings.TrimSpace(m.action.input), overwrite: overwrite})
}

func (m *appModel) startObservationCompare() tea.Cmd {
	ref, ok := m.captureObservationRef()
	if !ok {
		return nil
	}
	// Comparison uses whole Pods even when invoked from a container detail.
	ref.kind, ref.containerName = entityPod, ""
	row, ok := m.observationForRef(ref)
	if !ok {
		m.setActionError(fmt.Errorf("selected Pod is no longer observed"))
		return nil
	}
	if m.action.observationSource == nil {
		m.action.observationSource = &row
		m.action.observationSourceAt = m.data.Observations.ReceivedAt
		m.action.mode, m.action.err = actionResultMode, nil
		m.action.result = actionResult{title: "Comparison source marked", lines: []string{"First Pod: " + row.Namespace + "/" + row.Name, "Close this message, select another Pod, then press x again."}}
		return nil
	}
	before, beforeAt := m.action.observationSource, m.action.observationSourceAt
	m.action.observationSource = nil
	return m.startAction(actionRequest{kind: actionCompare, observationBefore: before, observationAfter: &row, beforeAt: beforeAt, afterAt: m.data.Observations.ReceivedAt})
}

func restrictedCompareResult(request actionRequest) (actionResult, error) {
	if request.observationBefore == nil || request.observationAfter == nil {
		return actionResult{}, fmt.Errorf("comparison requires two working-set observations")
	}
	lines, err := observationview.Compare(*request.observationBefore, *request.observationAfter, request.beforeAt, request.afterAt)
	return actionResult{title: "Restricted Pod comparison", lines: lines}, err
}

func restrictedCaptureResult(request actionRequest) (actionResult, error) {
	if request.outputPath == "" {
		return actionResult{}, fmt.Errorf("capture path must not be empty")
	}
	absolute, err := filepath.Abs(request.outputPath)
	if err != nil {
		return actionResult{}, fmt.Errorf("resolve capture path: %w", err)
	}
	if err := incident.WriteRestricted(io.Discard, absolute, request.overwrite, *request.restricted); err != nil {
		var exists incident.ExistsError
		if errors.As(err, &exists) {
			return actionResult{title: "Capture requires confirmation", outputPath: absolute, overwriteRequired: true}, err
		}
		return actionResult{}, err
	}
	return actionResult{title: "Redacted capture written", outputPath: absolute, lines: []string{"Path: " + absolute, "Mode: 0600", "Evidence: restricted / Kubernetes APIs", "Schema: 3", "Redacted: true", "History: unavailable; requires deep evidence"}}, nil
}
