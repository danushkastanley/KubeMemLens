package cli

import (
	"fmt"
	"sort"
	"text/tabwriter"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/client"
	"github.com/danushkastanley/kube-memlens/internal/model"
	"github.com/spf13/cobra"
)

func newHistoryCommand(collectorOptions collectorOptionsProvider) *cobra.Command {
	cmd := &cobra.Command{Use: "history", Short: "Show bounded recent memory history"}
	var namespace string
	var since time.Duration
	pod := &cobra.Command{
		Use:   "pod <pod-name>",
		Short: "Show recent memory points for one Pod",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if since < 0 || since > 24*time.Hour {
				return fmt.Errorf("--since must be between 0 and 24h")
			}
			opts := collectorOptions()
			reader, description, err := client.NewSnapshotReader(cmd.Context(), opts)
			if err != nil {
				return collectorUnavailableError(opts, description, err)
			}
			series, err := reader.PodHistory(cmd.Context(), namespace, args[0])
			if err != nil {
				return collectorUnavailableError(opts, description, err)
			}
			if len(series) == 0 {
				return fmt.Errorf("no recent history for Pod %s/%s", namespace, args[0])
			}
			if since > 0 {
				series = filterHistorySince(series, time.Now().UTC().Add(-since))
				if len(series) == 0 {
					return fmt.Errorf("no history for Pod %s/%s within %s", namespace, args[0], since)
				}
			}
			printPodHistory(cmd.OutOrStdout(), series)
			return nil
		},
	}
	pod.Flags().StringVarP(&namespace, "namespace", "n", "default", "Kubernetes namespace")
	pod.Flags().DurationVar(&since, "since", 0, "show points from the last duration, up to 24h (for example 5m)")
	cmd.AddCommand(pod)
	return cmd
}

func filterHistorySince(series []api.PodHistory, cutoff time.Time) []api.PodHistory {
	filtered := make([]api.PodHistory, 0, len(series))
	for _, item := range series {
		points := make([]api.MemoryHistoryPoint, 0, len(item.Points))
		for _, point := range item.Points {
			if !point.CapturedAt.Before(cutoff) {
				points = append(points, point)
			}
		}
		if len(points) == 0 {
			continue
		}
		item.Points = points
		filtered = append(filtered, item)
	}
	return filtered
}

func printPodHistory(w interface{ Write([]byte) (int, error) }, series []api.PodHistory) {
	for i := range series {
		sort.Slice(series[i].Points, func(a, b int) bool { return series[i].Points[a].CapturedAt.Before(series[i].Points[b].CapturedAt) })
		fmt.Fprintf(w, "Pod: %s/%s  Node: %s  Instance: %s\n", series[i].Namespace, series[i].PodName, series[i].NodeName, shortUID(series[i].PodUID))
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "TIME\tTOTAL\tANON\tCACHE\tSHMEM\tOTHER\tSWAP\tPSI SOME/FULL\tEVENTS\tRECLAIM")
		for _, point := range series[i].Points {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%.2f/%.2f\t%s\t%s\n",
				point.CapturedAt.Local().Format("15:04:05"),
				model.FormatCompactBytes(point.TotalBytes), model.FormatCompactBytes(point.AnonBytes),
				model.FormatCompactBytes(point.FileCacheBytes), model.FormatCompactBytes(point.ShmemBytes),
				model.FormatCompactBytes(point.ResidualBytes), model.FormatCompactBytes(point.SwapBytes),
				point.PSISomeAvg10, point.PSIFullAvg10, historyEvents(point), historyReclaim(point),
			)
		}
		_ = tw.Flush()
		if i < len(series)-1 {
			fmt.Fprintln(w)
		}
	}
}

func historyReclaim(point api.MemoryHistoryPoint) string {
	if !point.ReclaimDeltasKnown {
		return "baseline"
	}
	efficiency := "n/a"
	if point.PageScanDelta > 0 {
		efficiency = fmt.Sprintf("%.0f%%", float64(point.PageStealDelta)/float64(point.PageScanDelta)*100)
	}
	return fmt.Sprintf("scan=%d steal=%d eff=%s refault=%d major=%d",
		point.PageScanDelta, point.PageStealDelta, efficiency,
		point.RefaultAnonDelta+point.RefaultFileDelta, point.MajorFaultsDelta)
}

func historyEvents(point api.MemoryHistoryPoint) string {
	return fmt.Sprintf("oom=%d kill=%d high=%d max=%d", point.OOMEventsDelta, point.OOMKillEventsDelta, point.HighEventsDelta, point.MaxEventsDelta)
}

func shortUID(uid string) string {
	if len(uid) <= 12 {
		return uid
	}
	return uid[:12]
}
