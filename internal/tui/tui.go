package tui

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/go-logr/logr"
	"k8s.io/klog/v2"
)

func Run(ctx context.Context, opts Options) error {
	ctx = interactiveContext(ctx)
	reader := opts.SnapshotReader
	description := opts.ConnectionDescription
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
