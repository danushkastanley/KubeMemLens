package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/buildinfo"
	"github.com/danushkastanley/kube-memlens/internal/client"
	"github.com/spf13/cobra"
)

const maxCaptureHistoryPods = 100

func newCaptureCommand(collectorOptions collectorOptionsProvider) *cobra.Command {
	var output, namespace, podName string
	var includeHistory, includeSensitive, force bool
	cmd := &cobra.Command{
		Use:   "capture",
		Short: "Write a redacted incident bundle for offline replay",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			opts := collectorOptions()
			reader, description, err := client.NewSnapshotReader(cmd.Context(), opts)
			if err != nil {
				return collectorUnavailableError(opts, description, err)
			}
			pods, err := reader.Pods(cmd.Context())
			if err != nil {
				return collectorUnavailableError(opts, description, err)
			}
			pods = selectCapturePods(pods, namespace, podName)
			if podName != "" && len(pods) == 0 {
				return fmt.Errorf("Pod %s/%s was not found in current collector snapshots", namespace, podName)
			}
			nodes, err := reader.Nodes(cmd.Context())
			if err != nil {
				return collectorUnavailableError(opts, description, err)
			}
			bundle := api.IncidentBundle{
				SchemaVersion: api.CurrentIncidentSchemaVersion,
				CapturedAt:    time.Now().UTC(),
				ToolVersion:   buildinfo.Current(runtime.Version(), runtime.GOOS, runtime.GOARCH).String(),
				Redacted:      !includeSensitive,
				Pods:          pods,
				Nodes:         nodes,
			}
			if includeHistory {
				if len(pods) > maxCaptureHistoryPods {
					return fmt.Errorf("history capture is limited to %d Pods; use --namespace or --pod to narrow the bundle", maxCaptureHistoryPods)
				}
				for _, pod := range pods {
					history, historyErr := reader.PodHistory(cmd.Context(), pod.Namespace, pod.PodName)
					if historyErr != nil {
						return collectorUnavailableError(opts, description, historyErr)
					}
					bundle.Histories = append(bundle.Histories, history...)
				}
			}
			if bundle.Redacted {
				redactIncident(&bundle)
			}
			if err := writeIncidentBundle(cmd.OutOrStdout(), output, force, bundle); err != nil {
				return err
			}
			if output != "-" {
				fmt.Fprintf(cmd.OutOrStdout(), "Wrote %s (%d Pods, %d history series; redacted=%t)\n", output, len(bundle.Pods), len(bundle.Histories), bundle.Redacted)
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&output, "output", "o", "kube-memlens-incident.json", "output file, or - for stdout")
	cmd.Flags().StringVarP(&namespace, "namespace", "n", "", "capture only this namespace")
	cmd.Flags().StringVar(&podName, "pod", "", "capture only this Pod; requires --namespace")
	cmd.Flags().BoolVar(&includeHistory, "include-history", false, "include bounded recent history for captured Pods")
	cmd.Flags().BoolVar(&includeSensitive, "include-sensitive", false, "include Pod UIDs, container IDs, and cgroup paths")
	cmd.Flags().BoolVar(&force, "force", false, "replace an existing output file")
	cmd.PreRunE = func(_ *cobra.Command, _ []string) error {
		if podName != "" && namespace == "" {
			return fmt.Errorf("--pod requires --namespace")
		}
		if output == "" {
			return fmt.Errorf("--output must not be empty")
		}
		return nil
	}
	return cmd
}

func selectCapturePods(pods []api.PodSnapshot, namespace, podName string) []api.PodSnapshot {
	selected := make([]api.PodSnapshot, 0, len(pods))
	for _, pod := range pods {
		if namespace != "" && pod.Namespace != namespace {
			continue
		}
		if podName != "" && pod.PodName != podName {
			continue
		}
		selected = append(selected, pod)
	}
	return selected
}

func redactIncident(bundle *api.IncidentBundle) {
	for i := range bundle.Pods {
		bundle.Pods[i].PodUID = ""
		for j := range bundle.Pods[i].Containers {
			bundle.Pods[i].Containers[j].PodUID = ""
			bundle.Pods[i].Containers[j].ContainerID = ""
			bundle.Pods[i].Containers[j].CgroupPath = ""
			bundle.Pods[i].Containers[j].Context.Labels = nil
		}
		bundle.Pods[i].Context.Labels = nil
	}
	for i := range bundle.Histories {
		bundle.Histories[i].PodUID = ""
	}
}

func writeIncidentBundle(stdout io.Writer, output string, force bool, bundle api.IncidentBundle) error {
	if output == "-" {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(bundle)
	}
	if !force {
		if _, err := os.Stat(output); err == nil {
			return fmt.Errorf("output file %s already exists; use --force to replace it", output)
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("inspect output file %s: %w", output, err)
		}
	}
	dir := filepath.Dir(output)
	temp, err := os.CreateTemp(dir, ".kube-memlens-incident-*")
	if err != nil {
		return fmt.Errorf("create incident file: %w", err)
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	if err := temp.Chmod(0o600); err != nil {
		temp.Close()
		return fmt.Errorf("protect incident file: %w", err)
	}
	encoder := json.NewEncoder(temp)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(bundle); err != nil {
		temp.Close()
		return fmt.Errorf("encode incident file: %w", err)
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return fmt.Errorf("sync incident file: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close incident file: %w", err)
	}
	if force {
		if err := os.Remove(output); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("replace incident file: %w", err)
		}
	}
	if err := os.Rename(tempName, output); err != nil {
		return fmt.Errorf("publish incident file: %w", err)
	}
	return nil
}
