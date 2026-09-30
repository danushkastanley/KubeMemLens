package tui

import (
	"context"
	"errors"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/go-logr/logr"
	"k8s.io/klog/v2"
)

func Run(ctx context.Context, opts Options) error {
	ctx = interactiveContext(ctx)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	reader := opts.SnapshotReader
	description := opts.ConnectionDescription
	if opts.RefreshInterval <= 0 {
		opts.RefreshInterval = 5 * time.Second
	}
	model := newModel(ctx, opts, reader, description)
	program := tea.NewProgram(model, tea.WithContext(ctx))
	final, err := program.Run()
	if model, ok := final.(appModel); ok {
		return errors.Join(err, model.shutdownTrace())
	}
	return err
}

func interactiveContext(ctx context.Context) context.Context {
	return klog.NewContext(ctx, logr.Discard())
}
