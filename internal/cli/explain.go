package cli

import (
	"fmt"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/explain"
	"github.com/danushkastanley/kube-memlens/internal/model"
)

func newExplainCommand(collectorURL *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "explain",
		Short: "Explain Kubernetes memory usage",
	}

	var namespace string
	podCmd := &cobra.Command{
		Use:   "pod <pod-name>",
		Short: "Explain one pod",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			pods, err := fetchJSON[[]api.PodSnapshot](*collectorURL, "/api/v1/pods")
			if err != nil {
				return err
			}
			for _, pod := range pods {
				if pod.Namespace == namespace && pod.PodName == args[0] {
					printPodExplanation(cmd.OutOrStdout(), pod)
					return nil
				}
			}
			return fmt.Errorf("pod not found in collector snapshots. Check that the collector is reachable, the agent is posting snapshots, and the pod is running on a scanned node")
		},
	}
	podCmd.Flags().StringVarP(&namespace, "namespace", "n", "default", "Kubernetes namespace")
	cmd.AddCommand(podCmd)

	return cmd
}

func printPodExplanation(w interface{ Write([]byte) (int, error) }, pod api.PodSnapshot) {
	result := explain.Analyze(pod.Memory)
	printMemoryExplanation(w, fmt.Sprintf("Pod: %s/%s", pod.Namespace, pod.PodName), pod.Memory, result)
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Containers:")
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "CONTAINER\tTOTAL\tRSS\tCACHE\tSHMEM\tSLAB\tDIAGNOSIS")
	for _, container := range pod.Containers {
		fmt.Fprintf(
			tw,
			"%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			container.ContainerName,
			model.FormatCompactBytes(container.Memory.TotalBytes),
			model.FormatCompactBytes(container.Memory.RSSBytes()),
			model.FormatCompactBytes(container.Memory.CacheBytes()),
			model.FormatCompactBytes(container.Memory.ShmemBytes),
			model.FormatCompactBytes(container.Memory.SlabBytes),
			explain.Analyze(container.Memory).Diagnosis,
		)
	}
	_ = tw.Flush()
}
