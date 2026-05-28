package cli

import (
	"fmt"
	"sort"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/client"
	"github.com/danushkastanley/kube-memlens/internal/explain"
	"github.com/danushkastanley/kube-memlens/internal/model"
)

type collectorOptionsProvider func() client.Options

func newTopCommand(collectorOptions collectorOptionsProvider) *cobra.Command {
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
			opts := collectorOptions()
			reader, description, err := client.NewSnapshotReader(cmd.Context(), opts)
			if err != nil {
				return collectorUnavailableError(opts, description, err)
			}
			pods, err := reader.Pods(cmd.Context())
			if err != nil {
				return collectorUnavailableError(opts, description, err)
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

	var containerNamespace string
	var containerAllNamespaces bool
	containersCmd := &cobra.Command{
		Use:   "containers",
		Short: "Show container memory summaries",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			opts := collectorOptions()
			reader, description, err := client.NewSnapshotReader(cmd.Context(), opts)
			if err != nil {
				return collectorUnavailableError(opts, description, err)
			}
			containers, err := reader.Containers(cmd.Context())
			if err != nil {
				return collectorUnavailableError(opts, description, err)
			}
			if !containerAllNamespaces {
				containers = filterContainersByNamespace(containers, containerNamespace)
			}
			printContainersTable(cmd.OutOrStdout(), containers)
			return nil
		},
	}
	containersCmd.Flags().BoolVarP(&containerAllNamespaces, "all-namespaces", "A", false, "show containers across all namespaces")
	containersCmd.Flags().StringVarP(&containerNamespace, "namespace", "n", "default", "namespace to show when --all-namespaces is not set")
	cmd.AddCommand(containersCmd)

	cmd.AddCommand(&cobra.Command{
		Use:     "ns",
		Aliases: []string{"namespaces"},
		Short:   "Show namespace memory summaries",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			opts := collectorOptions()
			reader, description, err := client.NewSnapshotReader(cmd.Context(), opts)
			if err != nil {
				return collectorUnavailableError(opts, description, err)
			}
			namespaces, err := reader.Namespaces(cmd.Context())
			if err != nil {
				return collectorUnavailableError(opts, description, err)
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

func filterContainersByNamespace(containers []api.ContainerSnapshot, namespace string) []api.ContainerSnapshot {
	filtered := make([]api.ContainerSnapshot, 0, len(containers))
	for _, container := range containers {
		if container.Namespace == "" || container.PodName == "" || container.ContainerName == "" {
			continue
		}
		if container.Namespace == namespace {
			filtered = append(filtered, container)
		}
	}
	return filtered
}

func printPodsTable(w interface{ Write([]byte) (int, error) }, pods []api.PodSnapshot) {
	sort.Slice(pods, func(i, j int) bool {
		return pods[i].Memory.TotalBytes > pods[j].Memory.TotalBytes
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

func printContainersTable(w interface{ Write([]byte) (int, error) }, containers []api.ContainerSnapshot) {
	containers = mappedContainers(containers)
	sort.Slice(containers, func(i, j int) bool {
		return containers[i].Memory.TotalBytes > containers[j].Memory.TotalBytes
	})

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "NAMESPACE\tPOD\tCONTAINER\tNODE\tTOTAL\tRSS\tCACHE\tSHMEM\tSLAB\tDIAGNOSIS\tAGE")
	for _, container := range containers {
		fmt.Fprintf(
			tw,
			"%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			container.Namespace,
			container.PodName,
			container.ContainerName,
			container.NodeName,
			model.FormatCompactBytes(container.Memory.TotalBytes),
			model.FormatCompactBytes(container.Memory.RSSBytes()),
			model.FormatCompactBytes(container.Memory.CacheBytes()),
			model.FormatCompactBytes(container.Memory.ShmemBytes),
			model.FormatCompactBytes(container.Memory.SlabBytes),
			explain.Analyze(container.Memory).Diagnosis,
			formatAge(container.CapturedAt),
		)
	}
	_ = tw.Flush()
}

func mappedContainers(containers []api.ContainerSnapshot) []api.ContainerSnapshot {
	filtered := make([]api.ContainerSnapshot, 0, len(containers))
	for _, container := range containers {
		if container.Namespace == "" || container.PodName == "" || container.ContainerName == "" {
			continue
		}
		filtered = append(filtered, container)
	}
	return filtered
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
