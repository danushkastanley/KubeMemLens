package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/capability"
	"github.com/danushkastanley/kube-memlens/internal/client"
	"github.com/danushkastanley/kube-memlens/internal/explain"
	"github.com/danushkastanley/kube-memlens/internal/volumeview"
	"github.com/spf13/cobra"
)

type workloadVolumeExplanation struct {
	SchemaVersion int                          `json:"schemaVersion"`
	Kind          string                       `json:"kind"`
	Evidence      api.WorkloadVolumeContext    `json:"evidence"`
	Analysis      explain.WorkloadVolumeResult `json:"analysis"`
}

func newWorkloadVolumesCommand(options collectorOptionsProvider) *cobra.Command {
	var namespace, output string
	cmd := &cobra.Command{Use: "workload <kind>/<name>", Short: "Inspect live workload volume bindings and shared PVC filesystems", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		parts := strings.Split(args[0], "/")
		if len(parts) != 2 {
			return fmt.Errorf("workload must be <kind>/<name>")
		}
		return runWorkloadVolumes(cmd, options, namespace, parts[0], parts[1], output)
	}}
	cmd.Flags().StringVarP(&namespace, "namespace", "n", "default", "Kubernetes namespace")
	cmd.Flags().StringVarP(&output, "output", "o", "text", "output format: text, json, or yaml; authorised names are included")
	return cmd
}

func runWorkloadVolumes(cmd *cobra.Command, options collectorOptionsProvider, namespace, kind, name, output string) error {
	if err := validateNodeOutput(output); err != nil {
		return err
	}
	opts, err := withReadScope(options(), namespace, false)
	if err != nil {
		return err
	}
	if opts.EvidenceMode == capability.Restricted {
		return fmt.Errorf("workload volume context requires the authenticated collector workload volume profile")
	}
	reader, description, err := client.NewSnapshotReader(cmd.Context(), opts)
	if err != nil {
		return collectorUnavailableError(opts, description, err)
	}
	workloadReader, ok := reader.(client.WorkloadVolumeReader)
	if !ok {
		return fmt.Errorf("workload volume context requires the authenticated Kubernetes API connection")
	}
	value, err := workloadReader.WorkloadVolumes(cmd.Context(), namespace, kind, name)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	result := explain.AnalyzeWorkloadVolumes(value, now)
	if output != "text" {
		return writeNodeDocument(cmd.OutOrStdout(), output, workloadVolumeExplanation{volumeExplanationSchemaVersion, "WorkloadVolumeExplanation", value, result})
	}
	_, err = fmt.Fprintln(cmd.OutOrStdout(), strings.Join(volumeview.WorkloadLines(value, result, now, 100), "\n"))
	return err
}
