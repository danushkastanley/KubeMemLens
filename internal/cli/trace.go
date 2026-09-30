package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/capability"
	"github.com/danushkastanley/kube-memlens/internal/client"
	"github.com/danushkastanley/kube-memlens/internal/kube"
	"github.com/danushkastanley/kube-memlens/internal/trace"
	"github.com/danushkastanley/kube-memlens/internal/traceclient"
	"github.com/spf13/cobra"
)

type traceFactory func(client.Options) (*traceclient.Client, error)

func openTraceClient(options client.Options) (*traceclient.Client, error) {
	opts, err := options.WithDefaults()
	if err != nil {
		return nil, err
	}
	mode, err := client.ResolveMode(opts)
	if err != nil {
		return nil, err
	}
	if mode != client.ConnectionModeKubernetesAPI || opts.EvidenceMode == capability.Restricted || opts.CollectorURL != "" {
		return nil, fmt.Errorf("trace requires Kubernetes API access in auto or deep evidence mode")
	}
	config, err := kube.BuildConfig(opts.Kubeconfig, opts.Context)
	if err != nil {
		return nil, &traceclient.Error{Kind: traceclient.Configuration}
	}
	return traceclient.New(config)
}

func newTraceCommand(options collectorOptionsProvider) *cobra.Command {
	return newTraceCommandWithFactory(options, openTraceClient)
}
func newTraceCommandWithFactory(options collectorOptionsProvider, factory traceFactory) *cobra.Command {
	cmd := &cobra.Command{Use: "trace", Short: "Use the optional bounded trace extension", Long: "Request bounded development traces through the optional Kubernetes extension. Trace permission is separate from memory read access. Resource qualification remains incomplete."}
	for _, action := range []string{"preflight", "create", "run"} {
		cmd.AddCommand(newTraceTargetCommand(action, options, factory))
	}
	for _, action := range []string{"watch", "cancel", "export"} {
		cmd.AddCommand(newTraceIDCommand(action, options, factory))
	}
	return cmd
}

type traceTargetFlags struct {
	namespace, container, kind string
	duration                   time.Duration
	events                     uint64
}

func (f traceTargetFlags) intent() (traceclient.Intent, error) {
	intent := traceclient.DefaultIntent(trace.Kind(f.kind))
	intent.Bounds.Duration, intent.Bounds.Events = f.duration, f.events
	if err := trace.ValidateIntent(intent.Kind, intent.Paths, intent.Bounds); err != nil {
		return intent, err
	}
	if f.duration%time.Second != 0 {
		return intent, fmt.Errorf("trace duration must be a whole number of seconds")
	}
	if f.container == "" {
		return intent, fmt.Errorf("trace requires an explicit --container")
	}
	return intent, nil
}
func (f *traceTargetFlags) add(cmd *cobra.Command) {
	defaults := trace.DefaultBounds()
	cmd.Flags().StringVarP(&f.namespace, "namespace", "n", "default", "target namespace")
	cmd.Flags().StringVar(&f.container, "container", "", "exact running container name (required)")
	cmd.Flags().StringVar(&f.kind, "kind", "", "trace type: files, cache or oom (required)")
	cmd.Flags().DurationVar(&f.duration, "duration", defaults.Duration, "bounded observation duration (whole seconds, at most 5m)")
	cmd.Flags().Uint64Var(&f.events, "max-events", defaults.Events, "maximum event frames (1–100000; server policy may be lower)")
}
func traceContext(cmd *cobra.Command) (context.Context, context.CancelFunc) {
	return signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
}
