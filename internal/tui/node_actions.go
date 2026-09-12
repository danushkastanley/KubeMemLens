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
	"github.com/danushkastanley/kube-memlens/internal/nodeanalysis"
)

func (m *appModel) startNodeCapture(ref entityRef, overwrite bool) tea.Cmd {
	reader, ok := m.client.(incident.NodeCaptureReader)
	if !ok {
		m.setActionError(fmt.Errorf("Node capture requires the authenticated Kubernetes API connection"))
		return nil
	}
	return m.startAction(actionRequest{kind: actionCapture, ref: ref, nodeReader: reader, nodeRank: m.selectedNode.rank, outputPath: strings.TrimSpace(m.action.input), overwrite: overwrite})
}

func nodeCaptureResult(ctx context.Context, request actionRequest) (actionResult, error) {
	if request.outputPath == "" {
		return actionResult{}, fmt.Errorf("capture path must not be empty")
	}
	absolute, err := filepath.Abs(request.outputPath)
	if err != nil {
		return actionResult{}, err
	}
	version := buildinfo.Current(runtime.Version(), runtime.GOOS, runtime.GOARCH).String()
	bundle, err := incident.CollectNode(ctx, request.nodeReader, request.ref.nodeName, incident.NodeCaptureOptions{Rank: request.nodeRank, Limit: nodeanalysis.MaxContributors, IncludeHistory: true, ToolVersion: version})
	if err != nil {
		return actionResult{}, err
	}
	if err := incident.WriteNode(io.Discard, absolute, request.overwrite, bundle); err != nil {
		var exists incident.ExistsError
		if errors.As(err, &exists) {
			return actionResult{title: "Capture requires confirmation", outputPath: absolute, overwriteRequired: true}, err
		}
		return actionResult{}, err
	}
	return actionResult{title: "Redacted Node capture written", outputPath: absolute, lines: []string{"Path: " + absolute, "Mode: 0600; schema: 4; redacted: true", "Evidence fetched with current authorisation.", "Node UID fingerprint and contributor aliases retained.", "Replay with: kubectl memlens replay <path>"}}, nil
}
