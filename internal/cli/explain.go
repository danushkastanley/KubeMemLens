package cli

import (
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/client"
	"github.com/danushkastanley/kube-memlens/internal/explain"
	"github.com/danushkastanley/kube-memlens/internal/model"
)

func newExplainCommand(collectorOptions collectorOptionsProvider) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "explain",
		Short: "Explain Kubernetes memory usage",
	}

	var namespace string
	var podOutput string
	podCmd := &cobra.Command{
		Use:   "pod <pod-name>",
		Short: "Explain one pod",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if podOutput != "text" && podOutput != "json" && podOutput != "yaml" {
				return fmt.Errorf("invalid output %q, want text, json, or yaml", podOutput)
			}
			opts := collectorOptions()
			reader, description, err := client.NewSnapshotReader(cmd.Context(), opts)
			if err != nil {
				return collectorUnavailableError(opts, description, err)
			}
			pods, err := reader.Pods(cmd.Context())
			if err != nil {
				return collectorUnavailableError(opts, description, err)
			}
			for _, pod := range pods {
				if pod.Namespace == namespace && pod.PodName == args[0] {
					if podOutput == "text" {
						printPodExplanation(cmd.OutOrStdout(), pod)
						return nil
					}
					return writeExplanationDocument(cmd.OutOrStdout(), podOutput, podExplanationDocument(pod))
				}
			}
			return fmt.Errorf("pod not found in collector snapshots. Check that the collector is reachable, the agent is posting snapshots, and the pod is running on a scanned node")
		},
	}
	podCmd.Flags().StringVarP(&namespace, "namespace", "n", "default", "Kubernetes namespace")
	podCmd.Flags().StringVarP(&podOutput, "output", "o", "text", "output format: text, json, or yaml")
	cmd.AddCommand(podCmd)

	var workloadNamespace string
	var workloadOutput string
	workloadCmd := &cobra.Command{
		Use:   "workload <kind>/<name>",
		Short: "Explain one top-level workload and show replica outliers",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if workloadOutput != "text" && workloadOutput != "json" && workloadOutput != "yaml" {
				return fmt.Errorf("invalid output %q, want text, json, or yaml", workloadOutput)
			}
			parts := strings.Split(args[0], "/")
			if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
				return fmt.Errorf("workload must be written as <kind>/<name>, for example deployment/api")
			}
			opts := collectorOptions()
			reader, description, err := client.NewSnapshotReader(cmd.Context(), opts)
			if err != nil {
				return collectorUnavailableError(opts, description, err)
			}
			workloads, err := reader.Workloads(cmd.Context())
			if err != nil {
				return collectorUnavailableError(opts, description, err)
			}
			for _, workload := range workloads {
				if workload.Namespace == workloadNamespace && strings.EqualFold(workload.Kind, parts[0]) && workload.Name == parts[1] {
					if workloadOutput == "text" {
						printWorkloadExplanation(cmd.OutOrStdout(), workload)
						return nil
					}
					return writeExplanationDocument(cmd.OutOrStdout(), workloadOutput, workloadExplanationDocument(workload))
				}
			}
			return fmt.Errorf("workload %s/%s was not found in current collector snapshots", parts[0], parts[1])
		},
	}
	workloadCmd.Flags().StringVarP(&workloadNamespace, "namespace", "n", "default", "Kubernetes namespace")
	workloadCmd.Flags().StringVarP(&workloadOutput, "output", "o", "text", "output format: text, json, or yaml")
	cmd.AddCommand(workloadCmd)

	return cmd
}

func printWorkloadExplanation(w interface{ Write([]byte) (int, error) }, workload api.WorkloadSnapshot) {
	result := explain.AnalyzeWorkload(workload)
	printMemoryExplanation(w, fmt.Sprintf("Workload: %s/%s/%s", workload.Kind, workload.Namespace, workload.Name), workload.Memory, result)
	fmt.Fprintln(w)
	fmt.Fprintf(w, "Replicas: %d  Largest: %s (%s)\n\n", workload.PodCount, workload.LargestPodName, model.FormatCompactBytes(workload.LargestPodBytes))
	fmt.Fprintln(w, "Pods (largest first):")
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "POD\tNODE\tTOTAL\tLIMIT\tRSS\tCACHE\tSHMEM\tOTHER\tDIAGNOSIS")
	for _, pod := range workload.Pods {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			pod.PodName, pod.NodeName, model.FormatCompactBytes(pod.Memory.TotalBytes), limitUsage(pod.Memory),
			model.FormatCompactBytes(pod.Memory.RSSBytes()), model.FormatCompactBytes(pod.Memory.CacheBytes()),
			model.FormatCompactBytes(pod.Memory.ShmemBytes), model.FormatCompactBytes(pod.Memory.ResidualBytes()),
			explain.AnalyzePod(pod).Diagnosis,
		)
	}
	_ = tw.Flush()
	if workload.LargestPodName != "" {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Next commands:")
		for _, command := range workloadNextCommands(workload) {
			fmt.Fprintln(w, "- "+command)
		}
	}
}

func printPodExplanation(w interface{ Write([]byte) (int, error) }, pod api.PodSnapshot) {
	result := explain.AnalyzePod(pod)
	printMemoryExplanation(w, fmt.Sprintf("Pod: %s/%s", pod.Namespace, pod.PodName), pod.Memory, result)
	printPodContext(w, pod)
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Containers:")
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "CONTAINER\tTOTAL\tRSS\tCACHE\tSHMEM\tOTHER\tDIAGNOSIS")
	for _, container := range pod.Containers {
		fmt.Fprintf(
			tw,
			"%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			container.ContainerName,
			model.FormatCompactBytes(container.Memory.TotalBytes),
			model.FormatCompactBytes(container.Memory.RSSBytes()),
			model.FormatCompactBytes(container.Memory.CacheBytes()),
			model.FormatCompactBytes(container.Memory.ShmemBytes),
			model.FormatCompactBytes(container.Memory.ResidualBytes()),
			explain.AnalyzeContainer(container).Diagnosis,
		)
	}
	_ = tw.Flush()
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Next commands:")
	for _, command := range podNextCommands(pod) {
		fmt.Fprintln(w, "- "+command)
	}
}

func podNextCommands(pod api.PodSnapshot) []string {
	commands := []string{
		fmt.Sprintf("kubectl memlens history pod %s -n %s", pod.PodName, pod.Namespace),
		fmt.Sprintf("kubectl describe pod/%s -n %s", pod.PodName, pod.Namespace),
		fmt.Sprintf("kubectl get events -n %s --field-selector involvedObject.name=%s --sort-by=.lastTimestamp", pod.Namespace, pod.PodName),
	}
	if pod.Context.WorkloadKind != "" && pod.Context.WorkloadName != "" {
		commands = append(commands, fmt.Sprintf("kubectl memlens explain workload %s/%s -n %s", strings.ToLower(pod.Context.WorkloadKind), pod.Context.WorkloadName, pod.Namespace))
	}
	return commands
}

func workloadNextCommands(workload api.WorkloadSnapshot) []string {
	if workload.LargestPodName == "" {
		return nil
	}
	return []string{
		fmt.Sprintf("kubectl memlens explain pod %s -n %s", workload.LargestPodName, workload.Namespace),
		fmt.Sprintf("kubectl memlens history pod %s -n %s", workload.LargestPodName, workload.Namespace),
		fmt.Sprintf("kubectl describe %s/%s -n %s", strings.ToLower(workload.Kind), workload.Name, workload.Namespace),
	}
}
