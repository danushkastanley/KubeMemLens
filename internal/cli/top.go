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
	var podTopOptions topOptions
	podsCmd := &cobra.Command{
		Use:   "pods",
		Short: "Show pod memory summaries",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := podTopOptions.validate(); err != nil {
				return err
			}
			labelSelector, fieldSelector, err := parseSelectors(podTopOptions.LabelSelector, podTopOptions.FieldSelector)
			if err != nil {
				return err
			}
			opts, err := withReadScope(collectorOptions(), namespace, allNamespaces)
			if err != nil {
				return err
			}
			reader, description, err := client.NewSnapshotReader(cmd.Context(), opts)
			if err != nil {
				return collectorUnavailableError(opts, description, err)
			}
			return watchTop(cmd.Context(), cmd.OutOrStdout(), podTopOptions, func() error {
				pods, fetchErr := reader.Pods(cmd.Context())
				if fetchErr != nil {
					return collectorUnavailableError(opts, description, fetchErr)
				}
				if !allNamespaces {
					pods = filterPodsByNamespace(pods, namespace)
				}
				return writeTopRows(cmd.OutOrStdout(), podRows(pods, labelSelector, fieldSelector), podTopOptions, printPodRows)
			})
		},
	}
	podsCmd.Flags().BoolVarP(&allNamespaces, "all-namespaces", "A", false, "show pods across all namespaces")
	podsCmd.Flags().StringVarP(&namespace, "namespace", "n", "default", "namespace to show when --all-namespaces is not set")
	addTopFlags(podsCmd, &podTopOptions, true)
	cmd.AddCommand(podsCmd)

	var containerNamespace string
	var containerAllNamespaces bool
	var containerTopOptions topOptions
	containersCmd := &cobra.Command{
		Use:   "containers",
		Short: "Show container memory summaries",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := containerTopOptions.validate(); err != nil {
				return err
			}
			labelSelector, fieldSelector, err := parseSelectors(containerTopOptions.LabelSelector, containerTopOptions.FieldSelector)
			if err != nil {
				return err
			}
			opts, err := withReadScope(collectorOptions(), containerNamespace, containerAllNamespaces)
			if err != nil {
				return err
			}
			reader, description, err := client.NewSnapshotReader(cmd.Context(), opts)
			if err != nil {
				return collectorUnavailableError(opts, description, err)
			}
			return watchTop(cmd.Context(), cmd.OutOrStdout(), containerTopOptions, func() error {
				containers, fetchErr := reader.Containers(cmd.Context())
				if fetchErr != nil {
					return collectorUnavailableError(opts, description, fetchErr)
				}
				if !containerAllNamespaces {
					containers = filterContainersByNamespace(containers, containerNamespace)
				}
				return writeTopRows(cmd.OutOrStdout(), containerRows(containers, labelSelector, fieldSelector), containerTopOptions, printContainerRows)
			})
		},
	}
	containersCmd.Flags().BoolVarP(&containerAllNamespaces, "all-namespaces", "A", false, "show containers across all namespaces")
	containersCmd.Flags().StringVarP(&containerNamespace, "namespace", "n", "default", "namespace to show when --all-namespaces is not set")
	addTopFlags(containersCmd, &containerTopOptions, true)
	cmd.AddCommand(containersCmd)

	var workloadNamespace string
	var workloadAllNamespaces bool
	var workloadTopOptions topOptions
	workloadsCmd := &cobra.Command{
		Use:   "workloads",
		Short: "Show top-level workload memory summaries",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := workloadTopOptions.validate(); err != nil {
				return err
			}
			labelSelector, fieldSelector, err := parseSelectors(workloadTopOptions.LabelSelector, workloadTopOptions.FieldSelector)
			if err != nil {
				return err
			}
			opts, err := withReadScope(collectorOptions(), workloadNamespace, workloadAllNamespaces)
			if err != nil {
				return err
			}
			reader, description, err := client.NewSnapshotReader(cmd.Context(), opts)
			if err != nil {
				return collectorUnavailableError(opts, description, err)
			}
			return watchTop(cmd.Context(), cmd.OutOrStdout(), workloadTopOptions, func() error {
				workloads, fetchErr := reader.Workloads(cmd.Context())
				if fetchErr != nil {
					return collectorUnavailableError(opts, description, fetchErr)
				}
				if !workloadAllNamespaces {
					workloads = filterWorkloadsByNamespace(workloads, workloadNamespace)
				}
				return writeTopRows(cmd.OutOrStdout(), workloadRows(workloads, labelSelector, fieldSelector), workloadTopOptions, printWorkloadRows)
			})
		},
	}
	workloadsCmd.Flags().BoolVarP(&workloadAllNamespaces, "all-namespaces", "A", false, "show workloads across all namespaces")
	workloadsCmd.Flags().StringVarP(&workloadNamespace, "namespace", "n", "default", "namespace to show when --all-namespaces is not set")
	addTopFlags(workloadsCmd, &workloadTopOptions, true)
	cmd.AddCommand(workloadsCmd)

	var namespaceScope string
	var namespaceAllNamespaces bool
	var namespaceTopOptions topOptions
	namespacesCmd := &cobra.Command{
		Use:     "ns",
		Aliases: []string{"namespaces"},
		Short:   "Show namespace memory summaries",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := namespaceTopOptions.validate(); err != nil {
				return err
			}
			_, fieldSelector, err := parseSelectors("", namespaceTopOptions.FieldSelector)
			if err != nil {
				return err
			}
			opts, err := withReadScope(collectorOptions(), namespaceScope, namespaceAllNamespaces)
			if err != nil {
				return err
			}
			reader, description, err := client.NewSnapshotReader(cmd.Context(), opts)
			if err != nil {
				return collectorUnavailableError(opts, description, err)
			}
			return watchTop(cmd.Context(), cmd.OutOrStdout(), namespaceTopOptions, func() error {
				namespaces, fetchErr := reader.Namespaces(cmd.Context())
				if fetchErr != nil {
					return collectorUnavailableError(opts, description, fetchErr)
				}
				return writeTopRows(cmd.OutOrStdout(), namespaceRows(namespaces, fieldSelector), namespaceTopOptions, printNamespaceRows)
			})
		},
	}
	namespacesCmd.Flags().BoolVarP(&namespaceAllNamespaces, "all-namespaces", "A", false, "show all namespaces; requires cluster-viewer access")
	namespacesCmd.Flags().StringVarP(&namespaceScope, "namespace", "n", "default", "namespace to summarise")
	addTopFlags(namespacesCmd, &namespaceTopOptions, false)
	cmd.AddCommand(namespacesCmd)

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

func filterWorkloadsByNamespace(workloads []api.WorkloadSnapshot, namespace string) []api.WorkloadSnapshot {
	filtered := make([]api.WorkloadSnapshot, 0, len(workloads))
	for _, workload := range workloads {
		if workload.Namespace == namespace {
			filtered = append(filtered, workload)
		}
	}
	return filtered
}

func printPodsTable(w interface{ Write([]byte) (int, error) }, pods []api.PodSnapshot) {
	sort.Slice(pods, func(i, j int) bool {
		return pods[i].Memory.TotalBytes > pods[j].Memory.TotalBytes
	})

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "NAMESPACE\tPOD\tNODE\tTOTAL\tLIMIT\tRSS\tCACHE\tSHMEM\tOTHER\tDIAGNOSIS\tPOD AGE\tSAMPLE\tSTATE")
	for _, pod := range pods {
		printMemoryRow(tw, []string{
			pod.Namespace,
			pod.PodName,
			pod.NodeName,
			formatAge(pod.Context.CreatedAt),
			formatAge(pod.CapturedAt),
			string(pod.Freshness),
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
	fmt.Fprintln(tw, "NAMESPACE\tPOD\tCONTAINER\tNODE\tTOTAL\tLIMIT\tRSS\tCACHE\tSHMEM\tOTHER\tDIAGNOSIS\tSAMPLE\tSTATE")
	for _, container := range containers {
		fmt.Fprintf(
			tw,
			"%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			container.Namespace,
			container.PodName,
			container.ContainerName,
			container.NodeName,
			model.FormatCompactBytes(container.Memory.TotalBytes),
			limitUsage(container.Memory),
			model.FormatCompactBytes(container.Memory.RSSBytes()),
			model.FormatCompactBytes(container.Memory.CacheBytes()),
			model.FormatCompactBytes(container.Memory.ShmemBytes),
			model.FormatCompactBytes(container.Memory.ResidualBytes()),
			explain.Analyze(container.Memory).Diagnosis,
			formatAge(container.CapturedAt),
			container.Freshness,
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
	fmt.Fprintln(tw, "NAMESPACE\tPODS\tTOTAL\tRSS\tCACHE\tSHMEM\tOTHER\tDIAGNOSIS")
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
			model.FormatCompactBytes(namespace.Memory.ResidualBytes()),
			explain.Analyze(namespace.Memory).Diagnosis,
		)
	}
	_ = tw.Flush()
}

func printWorkloadsTable(w interface{ Write([]byte) (int, error) }, workloads []api.WorkloadSnapshot) {
	sort.Slice(workloads, func(i, j int) bool {
		return workloads[i].Memory.TotalBytes > workloads[j].Memory.TotalBytes
	})
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "NAMESPACE\tKIND\tWORKLOAD\tPODS\tTOTAL\tRSS\tCACHE\tSHMEM\tOTHER\tLARGEST POD\tMAX POD\tDIAGNOSIS")
	for _, workload := range workloads {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			workload.Namespace, workload.Kind, workload.Name, workload.PodCount,
			model.FormatCompactBytes(workload.Memory.TotalBytes), model.FormatCompactBytes(workload.Memory.RSSBytes()),
			model.FormatCompactBytes(workload.Memory.CacheBytes()), model.FormatCompactBytes(workload.Memory.ShmemBytes),
			model.FormatCompactBytes(workload.Memory.ResidualBytes()), workload.LargestPodName,
			model.FormatCompactBytes(workload.LargestPodBytes), explain.Analyze(workload.Memory).Diagnosis,
		)
	}
	_ = tw.Flush()
}

func printMemoryRow(tw *tabwriter.Writer, prefix []string, memory model.MemoryBreakdown) {
	fmt.Fprintf(
		tw,
		"%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
		prefix[0],
		prefix[1],
		prefix[2],
		model.FormatCompactBytes(memory.TotalBytes),
		limitUsage(memory),
		model.FormatCompactBytes(memory.RSSBytes()),
		model.FormatCompactBytes(memory.CacheBytes()),
		model.FormatCompactBytes(memory.ShmemBytes),
		model.FormatCompactBytes(memory.ResidualBytes()),
		explain.Analyze(memory).Diagnosis,
		prefix[3],
		prefix[4],
		prefix[5],
	)
}
