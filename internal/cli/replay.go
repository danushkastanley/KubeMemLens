package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/spf13/cobra"
)

const maxIncidentBytes int64 = 64 << 20

func newReplayCommand() *cobra.Command {
	var podRef string
	cmd := &cobra.Command{
		Use:   "replay <incident.json>",
		Short: "Replay a captured explanation without cluster access",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			bundle, err := readIncidentBundle(args[0])
			if err != nil {
				return err
			}
			if podRef != "" {
				pod, ok := incidentPod(bundle, podRef)
				if !ok {
					return fmt.Errorf("Pod %s was not found in the incident bundle", podRef)
				}
				printPodExplanation(cmd.OutOrStdout(), pod)
				printIncidentHistory(cmd.OutOrStdout(), bundle, pod)
				return nil
			}
			if len(bundle.Pods) == 1 {
				printPodExplanation(cmd.OutOrStdout(), bundle.Pods[0])
				printIncidentHistory(cmd.OutOrStdout(), bundle, bundle.Pods[0])
				return nil
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Incident captured: %s  Tool: %s  Redacted: %t\n\n", bundle.CapturedAt.Format(time.RFC3339), bundle.ToolVersion, bundle.Redacted)
			printPodsTable(cmd.OutOrStdout(), bundle.Pods)
			fmt.Fprintln(cmd.OutOrStdout(), "\nUse --pod <namespace>/<name> to replay one explanation.")
			return nil
		},
	}
	cmd.Flags().StringVar(&podRef, "pod", "", "replay one Pod as <namespace>/<name>")
	return cmd
}

func readIncidentBundle(path string) (api.IncidentBundle, error) {
	file, err := os.Open(path)
	if err != nil {
		return api.IncidentBundle{}, fmt.Errorf("open incident bundle: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return api.IncidentBundle{}, fmt.Errorf("inspect incident bundle: %w", err)
	}
	if info.Size() > maxIncidentBytes {
		return api.IncidentBundle{}, fmt.Errorf("incident bundle exceeds %d byte limit", maxIncidentBytes)
	}
	decoder := json.NewDecoder(io.LimitReader(file, maxIncidentBytes+1))
	decoder.DisallowUnknownFields()
	var bundle api.IncidentBundle
	if err := decoder.Decode(&bundle); err != nil {
		return api.IncidentBundle{}, fmt.Errorf("decode incident bundle: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return api.IncidentBundle{}, fmt.Errorf("decode incident bundle: unexpected trailing JSON")
	}
	if bundle.SchemaVersion != api.CurrentIncidentSchemaVersion {
		return api.IncidentBundle{}, fmt.Errorf("unsupported incident schemaVersion %d; expected %d", bundle.SchemaVersion, api.CurrentIncidentSchemaVersion)
	}
	if len(bundle.Pods) > 10_000 || len(bundle.Nodes) > 10_000 || len(bundle.Histories) > 10_000 {
		return api.IncidentBundle{}, fmt.Errorf("incident bundle exceeds entity limits")
	}
	return bundle, nil
}

func incidentPod(bundle api.IncidentBundle, ref string) (api.PodSnapshot, bool) {
	parts := strings.Split(ref, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return api.PodSnapshot{}, false
	}
	for _, pod := range bundle.Pods {
		if pod.Namespace == parts[0] && pod.PodName == parts[1] {
			return pod, true
		}
	}
	return api.PodSnapshot{}, false
}

func printIncidentHistory(w io.Writer, bundle api.IncidentBundle, pod api.PodSnapshot) {
	series := []api.PodHistory{}
	for _, history := range bundle.Histories {
		if history.Namespace == pod.Namespace && history.PodName == pod.PodName {
			series = append(series, history)
		}
	}
	if len(series) > 0 {
		fmt.Fprintln(w, "\nCaptured history:")
		printPodHistory(w, series)
	}
}
