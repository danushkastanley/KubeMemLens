package cli

import (
	"fmt"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/client"
	"github.com/danushkastanley/kube-memlens/internal/explain"
	"github.com/danushkastanley/kube-memlens/internal/model"
	"github.com/spf13/cobra"
)

func newCompareCommand(collectorOptions collectorOptionsProvider) *cobra.Command {
	var namespace, beforePath, afterPath, incidentPodRef, incidentWorkloadRef string
	cmd := &cobra.Command{
		Use:   "compare [pod-a] [pod-b]",
		Short: "Compare two live Pods or one Pod across incident bundles",
		Args: func(command *cobra.Command, args []string) error {
			if beforePath != "" || afterPath != "" {
				if len(args) != 0 {
					return fmt.Errorf("live Pod arguments cannot be combined with --before and --after")
				}
				return nil
			}
			return cobra.ExactArgs(2)(command, args)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if beforePath != "" || afterPath != "" {
				if beforePath == "" || afterPath == "" || (incidentPodRef == "") == (incidentWorkloadRef == "") {
					return fmt.Errorf("incident comparison requires --before, --after, and exactly one of --pod <namespace>/<name> or --workload <namespace>/<kind>/<name>")
				}
				before, err := readIncidentBundle(beforePath)
				if err != nil {
					return fmt.Errorf("read before bundle: %w", err)
				}
				after, err := readIncidentBundle(afterPath)
				if err != nil {
					return fmt.Errorf("read after bundle: %w", err)
				}
				if incidentPodRef != "" {
					beforePod, ok := incidentPod(before, incidentPodRef)
					if !ok {
						return fmt.Errorf("Pod %s was not found in the before bundle", incidentPodRef)
					}
					afterPod, ok := incidentPod(after, incidentPodRef)
					if !ok {
						return fmt.Errorf("Pod %s was not found in the after bundle", incidentPodRef)
					}
					printPodComparison(cmd.OutOrStdout(), "Incident comparison: "+incidentPodRef, beforePod, afterPod, after.CapturedAt.Sub(before.CapturedAt))
					return nil
				}
				beforeWorkload, ok := incidentWorkload(before, incidentWorkloadRef)
				if !ok {
					return fmt.Errorf("workload %s was not found in the before bundle", incidentWorkloadRef)
				}
				afterWorkload, ok := incidentWorkload(after, incidentWorkloadRef)
				if !ok {
					return fmt.Errorf("workload %s was not found in the after bundle", incidentWorkloadRef)
				}
				printPodComparison(cmd.OutOrStdout(), "Workload incident comparison: "+incidentWorkloadRef, beforeWorkload, afterWorkload, after.CapturedAt.Sub(before.CapturedAt))
				return nil
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
			leftName, err := livePodName(args[0])
			if err != nil {
				return err
			}
			rightName, err := livePodName(args[1])
			if err != nil {
				return err
			}
			left, leftOK := findPod(pods, namespace, leftName)
			right, rightOK := findPod(pods, namespace, rightName)
			if !leftOK || !rightOK {
				return fmt.Errorf("both Pods must exist in current %s namespace snapshots", namespace)
			}
			printPodComparison(cmd.OutOrStdout(), fmt.Sprintf("Live comparison: %s/%s → %s/%s", namespace, leftName, namespace, rightName), left, right, 0)
			return nil
		},
	}
	cmd.Flags().StringVarP(&namespace, "namespace", "n", "default", "namespace for live Pod comparison")
	cmd.Flags().StringVar(&beforePath, "before", "", "before incident bundle")
	cmd.Flags().StringVar(&afterPath, "after", "", "after incident bundle")
	cmd.Flags().StringVar(&incidentPodRef, "pod", "", "Pod to compare across bundles as <namespace>/<name>")
	cmd.Flags().StringVar(&incidentWorkloadRef, "workload", "", "workload to compare across bundles as <namespace>/<kind>/<name>")
	return cmd
}

func incidentWorkload(bundle api.IncidentBundle, reference string) (api.PodSnapshot, bool) {
	parts := strings.Split(reference, "/")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return api.PodSnapshot{}, false
	}
	memories := []model.MemoryBreakdown{}
	for _, pod := range bundle.Pods {
		if pod.Namespace == parts[0] && strings.EqualFold(pod.Context.WorkloadKind, parts[1]) && pod.Context.WorkloadName == parts[2] {
			memories = append(memories, pod.Memory)
		}
	}
	if len(memories) == 0 {
		return api.PodSnapshot{}, false
	}
	return api.PodSnapshot{Namespace: parts[0], PodName: parts[1] + "/" + parts[2], Memory: model.SumMemory(reference, memories)}, true
}

func livePodName(value string) (string, error) {
	parts := strings.Split(value, "/")
	if len(parts) == 1 && parts[0] != "" {
		return parts[0], nil
	}
	if len(parts) == 2 && strings.EqualFold(parts[0], "pod") && parts[1] != "" {
		return parts[1], nil
	}
	return "", fmt.Errorf("Pod %q must be a name or pod/<name>", value)
}

func findPod(pods []api.PodSnapshot, namespace, name string) (api.PodSnapshot, bool) {
	for _, pod := range pods {
		if pod.Namespace == namespace && pod.PodName == name {
			return pod, true
		}
	}
	return api.PodSnapshot{}, false
}

func printPodComparison(w interface{ Write([]byte) (int, error) }, title string, before, after api.PodSnapshot, elapsed time.Duration) {
	fmt.Fprintln(w, title)
	fmt.Fprintln(w)
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	if elapsed > 0 {
		fmt.Fprintln(tw, "SIGNAL\tBEFORE\tAFTER\tDELTA\tRATE")
	} else {
		fmt.Fprintln(tw, "SIGNAL\tBEFORE\tAFTER\tDELTA")
	}
	for _, row := range []struct {
		name        string
		beforeBytes uint64
		afterBytes  uint64
	}{
		{name: "Total", beforeBytes: before.Memory.TotalBytes, afterBytes: after.Memory.TotalBytes},
		{name: "RSS / anon", beforeBytes: before.Memory.RSSBytes(), afterBytes: after.Memory.RSSBytes()},
		{name: "File cache", beforeBytes: before.Memory.CacheBytes(), afterBytes: after.Memory.CacheBytes()},
		{name: "Shmem / tmpfs", beforeBytes: before.Memory.ShmemBytes, afterBytes: after.Memory.ShmemBytes},
		{name: "Residual / other", beforeBytes: before.Memory.ResidualBytes(), afterBytes: after.Memory.ResidualBytes()},
		{name: "Kernel", beforeBytes: before.Memory.KernelBytes, afterBytes: after.Memory.KernelBytes},
		{name: "Slab reclaimable", beforeBytes: before.Memory.SlabReclaimableBytes, afterBytes: after.Memory.SlabReclaimableBytes},
		{name: "Slab unreclaimable", beforeBytes: before.Memory.SlabUnreclaimableBytes, afterBytes: after.Memory.SlabUnreclaimableBytes},
		{name: "Socket memory", beforeBytes: before.Memory.SocketBytes, afterBytes: after.Memory.SocketBytes},
		{name: "Page tables", beforeBytes: before.Memory.PageTableBytes, afterBytes: after.Memory.PageTableBytes},
		{name: "Mapped file", beforeBytes: before.Memory.FileMappedBytes, afterBytes: after.Memory.FileMappedBytes},
		{name: "THP anon", beforeBytes: before.Memory.AnonTHPBytes, afterBytes: after.Memory.AnonTHPBytes},
		{name: "THP file", beforeBytes: before.Memory.FileTHPBytes, afterBytes: after.Memory.FileTHPBytes},
		{name: "THP shmem", beforeBytes: before.Memory.ShmemTHPBytes, afterBytes: after.Memory.ShmemTHPBytes},
		{name: "Swap", beforeBytes: before.Memory.SwapCurrentBytes, afterBytes: after.Memory.SwapCurrentBytes},
	} {
		if elapsed > 0 {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s/s\n", row.name, model.FormatCompactBytes(row.beforeBytes), model.FormatCompactBytes(row.afterBytes), signedBytes(row.beforeBytes, row.afterBytes), signedBytesRate(row.beforeBytes, row.afterBytes, elapsed))
		} else {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", row.name, model.FormatCompactBytes(row.beforeBytes), model.FormatCompactBytes(row.afterBytes), signedBytes(row.beforeBytes, row.afterBytes))
		}
	}
	fmt.Fprintf(tw, "PSI some avg10\t%.2f%%\t%.2f%%\t%+.2fpp\n", before.Memory.PSISomeAvg10, after.Memory.PSISomeAvg10, after.Memory.PSISomeAvg10-before.Memory.PSISomeAvg10)
	_ = tw.Flush()
	beforeResult, afterResult := explain.AnalyzePod(before), explain.AnalyzePod(after)
	fmt.Fprintf(w, "\nDiagnosis: %s (%s) → %s (%s)\n", beforeResult.Diagnosis, beforeResult.Confidence, afterResult.Diagnosis, afterResult.Confidence)
	if elapsed > 0 {
		fmt.Fprintf(w, "Elapsed: %s  Total rate: %s/s\n", elapsed.Round(time.Second), signedBytesRate(before.Memory.TotalBytes, after.Memory.TotalBytes, elapsed))
	}
}

func signedBytes(before, after uint64) string {
	if after >= before {
		return "+" + model.FormatCompactBytes(after-before)
	}
	return "-" + model.FormatCompactBytes(before-after)
}

func signedBytesRate(before, after uint64, elapsed time.Duration) string {
	if elapsed <= 0 {
		return "n/a"
	}
	seconds := elapsed.Seconds()
	if after >= before {
		return "+" + model.FormatCompactBytes(uint64(float64(after-before)/seconds))
	}
	return "-" + model.FormatCompactBytes(uint64(float64(before-after)/seconds))
}
