package tui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"runtime"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/danushkastanley/kube-memlens/internal/buildinfo"
	"github.com/danushkastanley/kube-memlens/internal/incident"
)

func (m *appModel) startVolumeCapture(overwrite bool) tea.Cmd {
	reader, ok := m.client.(incident.VolumeCaptureReader)
	if !ok {
		m.setActionError(fmt.Errorf("volume capture requires the authenticated Kubernetes API connection"))
		return nil
	}
	pod, ok := m.currentActionPod()
	if !ok {
		m.setActionError(fmt.Errorf("select a Pod for volume capture"))
		return nil
	}
	ref, _ := m.currentActionRef()
	return m.startAction(actionRequest{kind: actionCapture, ref: ref, volumeReader: reader, volumeExpectedUID: pod.PodUID, volumeHistory: len(m.selectedHistory.series) > 0, outputPath: strings.TrimSpace(m.action.input), overwrite: overwrite})
}

func volumeCaptureResult(ctx context.Context, request actionRequest) (actionResult, error) {
	if request.outputPath == "" {
		return actionResult{}, fmt.Errorf("capture path must not be empty")
	}
	absolute, err := filepath.Abs(request.outputPath)
	if err != nil {
		return actionResult{}, err
	}
	version := buildinfo.Current(runtime.Version(), runtime.GOOS, runtime.GOARCH).String()
	bundle, err := incident.CollectVolume(ctx, request.volumeReader, request.ref.namespace, request.ref.podName, incident.VolumeCaptureOptions{ExpectedUID: request.volumeExpectedUID, IncludeHistory: request.volumeHistory, ToolVersion: version})
	if err != nil {
		return actionResult{}, err
	}
	if err := incident.WriteVolume(io.Discard, absolute, request.overwrite, bundle); err != nil {
		var exists incident.ExistsError
		if errors.As(err, &exists) {
			return actionResult{title: "Capture requires confirmation", outputPath: absolute, overwriteRequired: true}, err
		}
		return actionResult{}, err
	}
	return actionResult{title: "Redacted volume capture written", outputPath: absolute, lines: []string{"Path: " + absolute, "Mode: 0600; schema: 5; redacted: true", "Evidence fetched with current authorisation.", "Capture-local aliases do not establish continuity across captures.", "Replay with: kubectl memlens replay <path>"}}, nil
}
