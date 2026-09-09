package tui

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/go-logr/logr"
	"k8s.io/klog/v2"

	"github.com/danushkastanley/kube-memlens/internal/client"
)

func Run(ctx context.Context, opts Options) error {
	ctx = interactiveContext(ctx)
	reader := opts.SnapshotReader
	description := opts.ConnectionDescription
	if reader == nil {
		var err error
		reader, description, err = client.NewSnapshotReader(ctx, opts.ConnectionOptions)
		if err != nil {
			return client.ConnectionError(opts.ConnectionOptions, description, err)
		}
	}
	if opts.RefreshInterval <= 0 {
		opts.RefreshInterval = 5 * time.Second
	}
	model := newModel(ctx, opts, reader, description)
	program := tea.NewProgram(model, tea.WithContext(ctx))
	_, err := program.Run()
	return err
}

func interactiveContext(ctx context.Context) context.Context {
	return klog.NewContext(ctx, logr.Discard())
}
