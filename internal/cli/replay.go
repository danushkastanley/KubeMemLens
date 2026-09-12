package cli

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/incident"
	"github.com/spf13/cobra"
)

const maxIncidentBytes int64 = incident.MaxBytes

func newReplayCommand() *cobra.Command {
	var podRef, nodeRef string
	var exportSchema int
	var exportOutput string
	var force bool
	cmd := &cobra.Command{
		Use:   "replay <incident.json>",
		Short: "Replay a captured explanation without cluster access",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if (exportSchema != 0 && exportSchema != 1 && exportSchema != 2) || (exportSchema == 0 && (exportOutput != "" || force)) || (exportSchema != 0 && exportOutput == "") {
				return fmt.Errorf("legacy export requires --export-schema 1 or 2 and --output")
			}
			if podRef != "" && nodeRef != "" {
				return fmt.Errorf("select either --pod or --node")
			}
			document, err := incident.Read(args[0])
			if err != nil {
				return err
			}
			if exportSchema != 0 {
				if document.Volume == nil {
					return fmt.Errorf("legacy volume export requires a schema-5 incident")
				}
				if nodeRef != "" || (podRef != "" && podRef != document.Volume.Pod.Namespace+"/"+document.Volume.Pod.PodName) {
					return fmt.Errorf("the selected target is not present in the volume incident")
				}
				legacy, err := incident.LegacyVolume(*document.Volume, exportSchema)
				if err != nil {
					return err
				}
				return incident.Write(cmd.OutOrStdout(), exportOutput, force, legacy)
			}
			if document.Volume != nil {
				if nodeRef != "" {
					return fmt.Errorf("volume incidents contain Pod evidence")
				}
				return replayVolume(cmd.OutOrStdout(), *document.Volume, podRef)
			}
			if document.Node != nil {
				if podRef != "" {
					return fmt.Errorf("schema 4 contains Node evidence; --pod is unavailable")
				}
				return replayNode(cmd.OutOrStdout(), *document.Node, nodeRef)
			}
			if nodeRef != "" {
				return fmt.Errorf("--node requires a schema-4 incident")
			}
			if document.Restricted != nil {
				return replayRestricted(cmd.OutOrStdout(), *document.Restricted, podRef)
			}
			bundle := *document.Deep
			for _, caveat := range bundle.Caveats {
				fmt.Fprintf(cmd.OutOrStdout(), "Capture caveat: %q\n", caveat)
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
	cmd.Flags().StringVar(&nodeRef, "node", "", "replay the selected Node from a schema-4 incident")
	cmd.Flags().IntVar(&exportSchema, "export-schema", 0, "explicitly export a volume incident as legacy schema 1 or 2, omitting volume and I/O enrichment")
	cmd.Flags().StringVarP(&exportOutput, "output", "o", "", "legacy export file, or - for stdout")
	cmd.Flags().BoolVar(&force, "force", false, "replace an existing legacy export file")
	return cmd
}

func readIncidentBundle(path string) (api.IncidentBundle, error) {
	document, err := incident.Read(path)
	if err != nil {
		return api.IncidentBundle{}, err
	}
	if document.Deep == nil {
		return api.IncidentBundle{}, fmt.Errorf("this incident cannot be read as deep Pod evidence")
	}
	return *document.Deep, nil
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
