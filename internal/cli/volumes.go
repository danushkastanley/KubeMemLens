package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/capability"
	"github.com/danushkastanley/kube-memlens/internal/client"
	"github.com/danushkastanley/kube-memlens/internal/explain"
	"github.com/danushkastanley/kube-memlens/internal/volumecontext"
	"github.com/danushkastanley/kube-memlens/internal/volumeview"
	"github.com/spf13/cobra"
)

const volumeExplanationSchemaVersion = 5

type volumeExplanation struct {
	SchemaVersion int                  `json:"schemaVersion"`
	Kind          string               `json:"kind"`
	EvaluatedAt   time.Time            `json:"evaluatedAt"`
	Context       volumecontext.View   `json:"context"`
	Analysis      explain.VolumeResult `json:"analysis"`
}

func newVolumesCommand(options collectorOptionsProvider) *cobra.Command {
	cmd := &cobra.Command{Use: "volumes", Short: "Inspect authorised filesystem and CSI health beside memory evidence"}
	var namespace, output string
	podCmd := &cobra.Command{Use: "pod <pod-name>", Short: "Show volume configuration, usage, health and memory correlation", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if err := validateNodeOutput(output); err != nil {
			return err
		}
		opts, err := withReadScope(options(), namespace, false)
		if err != nil {
			return err
		}
		session, err := currentPodSession(cmd.Context(), opts, args[0])
		if err != nil {
			return collectorUnavailableError(opts, session.Description, err)
		}
		if session.Plan.Mode == capability.Restricted {
			return fmt.Errorf("volume correlation requires the authenticated collector volume profile; restricted mode retains its separate memory evidence")
		}
		reader, ok := session.Reader.(client.PodVolumeReader)
		if !ok {
			return fmt.Errorf("volume correlation requires the authenticated Kubernetes API connection and optional volume profile")
		}
		pod, volumes, err := client.ReadPodVolumeEvidence(cmd.Context(), reader, namespace, args[0], "")
		if err != nil {
			return err
		}
		return writePodVolumeExplanation(cmd, output, pod, volumes)
	}}
	podCmd.Flags().StringVarP(&namespace, "namespace", "n", "default", "Kubernetes namespace")
	podCmd.Flags().StringVarP(&output, "output", "o", "text", "output format: text, json, or yaml; authorised names are included")
	cmd.AddCommand(podCmd)
	cmd.AddCommand(newWorkloadVolumesCommand(options))
	return cmd
}

func writePodVolumeExplanation(cmd *cobra.Command, output string, pod api.PodSnapshot, volumes api.PodVolumeContext) error {
	now := time.Now().UTC()
	result := explain.AnalyzeVolumes(explain.VolumeInput{Pod: pod, Volumes: volumes.Context, Now: now})
	if output != "text" {
		return writeNodeDocument(cmd.OutOrStdout(), output, volumeExplanation{SchemaVersion: volumeExplanationSchemaVersion, Kind: "PodVolumeExplanation", EvaluatedAt: now, Context: volumes.Context, Analysis: result})
	}
	printPodExplanation(cmd.OutOrStdout(), pod)
	if _, err := fmt.Fprintln(cmd.OutOrStdout(), "\n"+strings.Join(volumeview.Lines(volumes.Context, result, now, 100), "\n")); err != nil {
		return err
	}
	fmt.Fprintln(cmd.OutOrStdout(), "\nRead-only follow-up commands:")
	for _, command := range volumeview.Commands(volumes.Context) {
		if _, err := fmt.Fprintln(cmd.OutOrStdout(), command); err != nil {
			return err
		}
	}
	return nil
}

func explainPodWithVolumes(cmd *cobra.Command, reader client.SnapshotReader, pod api.PodSnapshot, output string) error {
	volumeReader, ok := reader.(client.PodVolumeReader)
	if !ok {
		return fmt.Errorf("volume correlation requires the authenticated Kubernetes API connection and optional volume profile")
	}
	current, volumes, err := client.ReadPodVolumeEvidence(cmd.Context(), volumeReader, pod.Namespace, pod.PodName, pod.PodUID)
	if err != nil {
		return err
	}
	return writePodVolumeExplanation(cmd, output, current, volumes)
}
