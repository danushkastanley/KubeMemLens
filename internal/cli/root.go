package cli

import (
	"fmt"
	"io"

	"github.com/danushkastanley/kube-memlens/internal/capability"
	"github.com/danushkastanley/kube-memlens/internal/client"
	"github.com/spf13/cobra"
)

type collectorFlags struct {
	evidenceMode       string
	connectMode        string
	collectorURL       string
	collectorNamespace string
	collectorService   string
	collectorPort      int
	kubeconfig         string
	context            string
}

func NewRootCommand(stdout, stderr io.Writer) *cobra.Command {
	defaults := client.DefaultOptions()
	flags := &collectorFlags{
		evidenceMode:       string(capability.Auto),
		connectMode:        string(defaults.Mode),
		collectorURL:       defaults.CollectorURL,
		collectorNamespace: defaults.CollectorNamespace,
		collectorService:   defaults.CollectorService,
		collectorPort:      defaults.CollectorPort,
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
	cmd.PersistentFlags().StringVar(&flags.evidenceMode, "mode", flags.evidenceMode, "evidence mode: auto, deep or restricted; status reports available sources")
	cmd.PersistentFlags().StringVar(&flags.connectMode, "connect-mode", flags.connectMode, "collector connection mode: auto, kubernetes-api, http, or kube-proxy")
	cmd.PersistentFlags().StringVar(&flags.collectorURL, "collector-url", flags.collectorURL, "collector base URL for HTTP mode")
	cmd.PersistentFlags().StringVar(&flags.collectorNamespace, "collector-namespace", flags.collectorNamespace, "collector service namespace for kube-proxy mode")
	cmd.PersistentFlags().StringVar(&flags.collectorService, "collector-service", flags.collectorService, "collector service name for kube-proxy mode")
	cmd.PersistentFlags().IntVar(&flags.collectorPort, "collector-port", flags.collectorPort, "collector service port for kube-proxy mode")
	cmd.PersistentFlags().StringVar(&flags.kubeconfig, "kubeconfig", "", "path to the kubeconfig file")
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

	return cmd
}

func Execute(stdout, stderr io.Writer) error {
	cmd := NewRootCommand(stdout, stderr)
	if err := cmd.Execute(); err != nil {
		return fmt.Errorf("%w", err)
	}

	return nil
}

func (f *collectorFlags) options() client.Options {
	return client.Options{
		EvidenceMode:       capability.Mode(f.evidenceMode),
		Mode:               client.ConnectionMode(f.connectMode),
		CollectorURL:       f.collectorURL,
		CollectorNamespace: f.collectorNamespace,
		CollectorService:   f.collectorService,
		CollectorPort:      f.collectorPort,
		Kubeconfig:         f.kubeconfig,
		Context:            f.context,
	}
}
