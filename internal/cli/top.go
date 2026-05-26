package cli

import (
	"fmt"
	"sort"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/explain"
	"github.com/danushkastanley/kube-memlens/internal/model"
)

func newTopCommand(collectorURL *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "top",
		Short: "Show Kubernetes memory summaries",
	}

	var namespace string
	var allNamespaces bool
	podsCmd := &cobra.Command{
		Use:   "pods",
		Short: "Show pod memory summaries",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			pods, err := fetchJSON[[]api.PodSnapshot](*collectorURL, "/api/v1/pods")
			if err != nil {
				return err
			}
			if !allNamespaces {
				pods = filterPodsByNamespace(pods, namespace)
			}
			printPodsTable(cmd.OutOrStdout(), pods)
			return nil
		},
	}
	podsCmd.Flags().BoolVarP(&allNamespaces, "all-namespaces", "A", false, "show pods across all namespaces")
	podsCmd.Flags().StringVarP(&namespace, "namespace", "n", "default", "namespace to show when --all-namespaces is not set")
	cmd.AddCommand(podsCmd)

	cmd.AddCommand(&cobra.Command{
		Use:     "ns",
		Aliases: []string{"namespaces"},
		Short:   "Show namespace memory summaries",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			namespaces, err := fetchJSON[[]api.NamespaceSnapshot](*collectorURL, "/api/v1/namespaces")
			if err != nil {
				return err
			}
			printNamespacesTable(cmd.OutOrStdout(), namespaces)
			return nil
		},
	})

	return cmd
}

func filterPodsByNamespace(pods []api.PodSnapshot, namespace string) []api.PodSnapshot {
	filtered := make([]api.PodSnapshot, 0, len(pods))
	for _, pod := range pods {
		if pod.Namespace == namespace {
			filtered = append(filtered, pod)
		}
	}
	return filtered
}

func printPodsTable(w interface{ Write([]byte) (int, error) }, pods []api.PodSnapshot) {
	sort.Slice(pods, func(i, j int) bool {
		if pods[i].Namespace == pods[j].Namespace {
			return pods[i].PodName < pods[j].PodName
		}
		return pods[i].Namespace < pods[j].Namespace
	})

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "NAMESPACE\tPOD\tNODE\tTOTAL\tRSS\tCACHE\tSHMEM\tSLAB\tDIAGNOSIS\tAGE")
	for _, pod := range pods {
		printMemoryRow(tw, []string{
			pod.Namespace,
			pod.PodName,
			pod.NodeName,
			formatAge(pod.CapturedAt),
		}, pod.Memory)
	}
	_ = tw.Flush()
}

func printNamespacesTable(w interface{ Write([]byte) (int, error) }, namespaces []api.NamespaceSnapshot) {
	sort.Slice(namespaces, func(i, j int) bool {
		return namespaces[i].Namespace < namespaces[j].Namespace
	})

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "NAMESPACE\tPODS\tTOTAL\tRSS\tCACHE\tSHMEM\tSLAB\tDIAGNOSIS")
	for _, namespace := range namespaces {
		fmt.Fprintf(
			tw,
			"%s\t%d\t%s\t%s\t%s\t%s\t%s\t%s\n",
			namespace.Namespace,
			namespace.PodCount,
			model.FormatCompactBytes(namespace.Memory.TotalBytes),
			model.FormatCompactBytes(namespace.Memory.RSSBytes()),
			model.FormatCompactBytes(namespace.Memory.CacheBytes()),
			model.FormatCompactBytes(namespace.Memory.ShmemBytes),
			model.FormatCompactBytes(namespace.Memory.SlabBytes),
			explain.Analyze(namespace.Memory).Diagnosis,
		)
	}
	_ = tw.Flush()
}

func printMemoryRow(tw *tabwriter.Writer, prefix []string, memory model.MemoryBreakdown) {
	fmt.Fprintf(
		tw,
		"%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
		prefix[0],
		prefix[1],
		prefix[2],
		model.FormatCompactBytes(memory.TotalBytes),
		model.FormatCompactBytes(memory.RSSBytes()),
		model.FormatCompactBytes(memory.CacheBytes()),
		model.FormatCompactBytes(memory.ShmemBytes),
		model.FormatCompactBytes(memory.SlabBytes),
		explain.Analyze(memory).Diagnosis,
		prefix[3],
	)
}
