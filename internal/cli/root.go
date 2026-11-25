package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/capability"
	"github.com/danushkastanley/kube-memlens/internal/client"
	"github.com/spf13/cobra"
)

type collectorFlags struct {
	evidenceMode string
	connectMode  string
	collectorURL string
	// environmentCollectorURL is MEMLENS_COLLECTOR_URL as read at start-up.
	environmentCollectorURL string
	collectorNamespace      string
	collectorService        string
	collectorPort           int
	kubeconfig              string
	context                 string
	requestTimeout          time.Duration
}

func NewRootCommand(stdout, stderr io.Writer) *cobra.Command {
	cmd, _ := newRootCommand(stdout, stderr)
	return cmd
}

func newRootCommand(stdout, stderr io.Writer) (*cobra.Command, *collectorFlags) {
	defaults := client.DefaultOptions()
	flags := &collectorFlags{
		evidenceMode: string(capability.Auto),
		connectMode:  string(defaults.Mode),
		// The variable supplies the address for --connect-mode=http. It does
		// not choose that mode: see connection_notice.go.
		environmentCollectorURL: defaults.CollectorURL,
		collectorNamespace:      defaults.CollectorNamespace,
		collectorService:        defaults.CollectorService,
		collectorPort:           defaults.CollectorPort,
	}
	cmd := &cobra.Command{
		Use:   "kubectl-memlens",
		Short: "Terminal-first Kubernetes memory inspector",
		Long: "KubeMemLens helps explain why pod or container memory is high by separating cgroup memory " +
			"into non-overlapping anonymous memory, filesystem cache, tmpfs, and residual/other buckets, with kernel, writeback, and pressure drill-down evidence.",
		SilenceUsage:  true,
		SilenceErrors: true,
		// Show the kubectl plugin invocation in usage and help text.
		Annotations: map[string]string{cobra.CommandDisplayNameAnnotation: "kubectl memlens"},
	}

	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.PersistentPreRunE = func(cmd *cobra.Command, _ []string) error {
		if notice := flags.connectionNotice(); notice != "" {
			fmt.Fprintln(cmd.ErrOrStderr(), notice)
		}
		if err := flags.checkRequestTimeout(); err != nil {
			return err
		}
		flags.applyContextNamespace(cmd)
		return nil
	}
	cmd.PersistentFlags().StringVar(&flags.evidenceMode, "mode", flags.evidenceMode, "evidence mode: auto, deep or restricted; status reports available sources")
	cmd.PersistentFlags().StringVar(&flags.connectMode, "connect-mode", flags.connectMode, "collector connection mode: auto, kubernetes-api, http, or kube-proxy")
	cmd.PersistentFlags().StringVar(&flags.collectorURL, "collector-url", "", "collector base URL for unauthenticated HTTP mode; MEMLENS_COLLECTOR_URL is used only with --connect-mode=http")
	cmd.PersistentFlags().StringVar(&flags.collectorNamespace, "collector-namespace", flags.collectorNamespace, "collector service namespace for kube-proxy mode")
	cmd.PersistentFlags().StringVar(&flags.collectorService, "collector-service", flags.collectorService, "collector service name for kube-proxy mode")
	cmd.PersistentFlags().IntVar(&flags.collectorPort, "collector-port", flags.collectorPort, "collector service port for kube-proxy mode")
	cmd.PersistentFlags().StringVar(&flags.kubeconfig, "kubeconfig", "", "path to the kubeconfig file")
	cmd.PersistentFlags().DurationVar(&flags.requestTimeout, "request-timeout", defaults.Timeout, "time allowed for one read from the cluster, from 1s to 5m")
	cmd.PersistentFlags().StringVar(&flags.context, "context", "", "kubeconfig context to use")
	// These flags exist only for controlled rollback of pre-v1 installations.
	// They keep working but are hidden so new users see the supported path.
	for _, name := range []string{"connect-mode", "collector-url", "collector-namespace", "collector-service", "collector-port"} {
		if err := cmd.PersistentFlags().MarkHidden(name); err != nil {
			panic(fmt.Sprintf("hide pre-v1 flag %s: %v", name, err))
		}
	}

	cmd.AddCommand(newSampleCommand())
	cmd.AddCommand(newTopCommand(flags.options))
	cmd.AddCommand(newExplainCommand(flags.options))
	cmd.AddCommand(newVolumesCommand(flags.options))
	cmd.AddCommand(newTUICommand(flags.options))
	cmd.AddCommand(newTraceCommand(flags.options))
	cmd.AddCommand(newStatusCommand(flags.options))
	cmd.AddCommand(newDoctorCommand(flags.options))
	cmd.AddCommand(newHistoryCommand(flags.options))
	cmd.AddCommand(newReplicasCommand(flags.options))
	cmd.AddCommand(newIncidentSessionCommand(flags.options))
	cmd.AddCommand(newCaptureCommand(flags.options))
	cmd.AddCommand(newReplayCommand())
	cmd.AddCommand(newCompareCommand(flags.options))
	cmd.AddCommand(newRecommendCommand(flags.options))
	cmd.AddCommand(newVersionCommand())
	describeContextNamespace(cmd)

	return cmd, flags
}

// ErrInterrupted is returned when the command line was stopped by Ctrl+C or a
// termination signal. Whatever the command reported after that describes the
// cancelled read, not a fault, so the caller should exit without printing it.
var ErrInterrupted = errors.New("interrupted")

func Execute(stdout, stderr io.Writer) error {
	ctx, stop := interruptContext(context.Background())
	defer stop()
	return interruptedOr(ctx, ExecuteContext(ctx, stdout, stderr))
}

func interruptedOr(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ErrInterrupted
	}
	return err
}

// ExecuteContext runs the command line until it finishes or ctx is cancelled.
// Commands read cmd.Context(), so cancelling it ends a read in progress and
// lets a command remove what it has staged.
func ExecuteContext(ctx context.Context, stdout, stderr io.Writer) error {
	cmd := NewRootCommand(stdout, stderr)
	if err := cmd.ExecuteContext(ctx); err != nil {
		return fmt.Errorf("%w", err)
	}

	return nil
}

func (f *collectorFlags) options() client.Options {
	return client.Options{
		EvidenceMode:       capability.Mode(f.evidenceMode),
		Mode:               client.ConnectionMode(f.connectMode),
		CollectorURL:       f.effectiveCollectorURL(),
		CollectorNamespace: f.collectorNamespace,
		CollectorService:   f.collectorService,
		CollectorPort:      f.collectorPort,
		Kubeconfig:         f.kubeconfig,
		Context:            f.context,
		Timeout:            f.requestTimeout,
	}
}
