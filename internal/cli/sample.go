package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/danushkastanley/kube-memlens/internal/cgroup"
	"github.com/danushkastanley/kube-memlens/internal/explain"
	"github.com/danushkastanley/kube-memlens/internal/model"
)

const defaultSampleRoot = "examples/cgroup-v2"

var preferredSampleOrder = []string{
	"cache-heavy",
	"rss-heavy",
	"tmpfs-heavy",
	"dirty-heavy",
	"normal",
}

func newSampleCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sample",
		Short: "Use local sample cgroup data",
	}

	cmd.AddCommand(newSampleListCommand())
	cmd.AddCommand(newSampleExplainCommand())
	cmd.AddCommand(newSampleTopCommand())

	return cmd
}

func newSampleListCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List available samples",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			samples, err := listSamples(sampleRoot())
			if err != nil {
				return err
			}

			fmt.Fprintln(cmd.OutOrStdout(), "Available samples:")
			for _, sample := range samples {
				fmt.Fprintf(cmd.OutOrStdout(), "  %s\n", sample)
			}
			return nil
		},
	}
}

func newSampleExplainCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "explain <sample-name>",
		Short: "Explain one local cgroup sample",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			breakdown, err := loadSample(args[0])
			if err != nil {
				return err
			}

			result := explain.Analyze(breakdown)
			printExplanation(cmd.OutOrStdout(), breakdown, result)
			return nil
		},
	}
}

func newSampleTopCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "top",
		Short: "Show a table of local cgroup samples",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			samples, err := listSamples(sampleRoot())
			if err != nil {
				return err
			}

			rows := make([]model.MemoryBreakdown, 0, len(samples))
			for _, sample := range samples {
				breakdown, err := loadSample(sample)
				if err != nil {
					return err
				}
				rows = append(rows, breakdown)
			}

			printSampleTop(cmd.OutOrStdout(), rows)
			return nil
		},
	}
}

func sampleRoot() string {
	if root := strings.TrimSpace(os.Getenv("KUBEMEMLENS_SAMPLE_ROOT")); root != "" {
		return root
	}

	return defaultSampleRoot
}

func listSamples(root string) ([]string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("list samples under %s: %w", root, err)
	}

	available := map[string]struct{}{}
	for _, entry := range entries {
		if entry.IsDir() {
			available[entry.Name()] = struct{}{}
		}
	}

	samples := make([]string, 0, len(available))
	for _, sample := range preferredSampleOrder {
		if _, ok := available[sample]; ok {
			samples = append(samples, sample)
			delete(available, sample)
		}
	}

	extra := make([]string, 0, len(available))
	for sample := range available {
		extra = append(extra, sample)
	}
	sort.Strings(extra)
	samples = append(samples, extra...)

	return samples, nil
}

func loadSample(name string) (model.MemoryBreakdown, error) {
	if strings.Contains(name, string(os.PathSeparator)) || name == "." || name == ".." {
		return model.MemoryBreakdown{}, fmt.Errorf("invalid sample name %q", name)
	}

	dir := filepath.Join(sampleRoot(), name)
	breakdown, err := cgroup.ParseDirectory(name, dir)
	if err != nil {
		return model.MemoryBreakdown{}, fmt.Errorf("load sample %s: %w", name, err)
	}

	return breakdown, nil
}

func printExplanation(w interface{ Write([]byte) (int, error) }, breakdown model.MemoryBreakdown, result explain.Result) {
	printMemoryExplanation(w, fmt.Sprintf("Sample: %s", breakdown.Name), breakdown, result)
}

func printMemoryExplanation(w interface{ Write([]byte) (int, error) }, title string, breakdown model.MemoryBreakdown, result explain.Result) {
	fmt.Fprintf(w, "%s\n\n", title)
	fmt.Fprintf(w, "Total charged memory: %s\n", model.FormatBytes(breakdown.TotalBytes))
	fmt.Fprintf(w, "RSS / anon:           %s\n", model.FormatBytes(breakdown.RSSBytes()))
	fmt.Fprintf(w, "File cache:           %s\n", model.FormatBytes(breakdown.CacheBytes()))
	fmt.Fprintf(w, "Active file:          %s\n", model.FormatBytes(breakdown.ActiveFileBytes))
	fmt.Fprintf(w, "Inactive file:        %s\n", model.FormatBytes(breakdown.InactiveFileBytes))
	fmt.Fprintf(w, "Shmem / tmpfs:        %s\n", model.FormatBytes(breakdown.ShmemBytes))
	fmt.Fprintf(w, "Slab / kernel:        %s\n", model.FormatBytes(breakdown.SlabBytes))
	fmt.Fprintf(w, "Dirty/writeback:      %s\n", model.FormatDirtyWriteback(breakdown))
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Diagnosis:")
	fmt.Fprintf(w, "%s\n\n", result.Diagnosis)
	fmt.Fprintln(w, "Likely explanation:")
	fmt.Fprintf(w, "%s\n\n", result.LikelyExplanation)
	fmt.Fprintln(w, "Suggested checks:")
	for _, check := range result.SuggestedChecks {
		fmt.Fprintf(w, "- %s\n", check)
	}
}

func printSampleTop(w interface{ Write([]byte) (int, error) }, rows []model.MemoryBreakdown) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tTOTAL\tRSS\tCACHE\tSHMEM\tSLAB\tDIAGNOSIS")
	for _, row := range rows {
		fmt.Fprintf(
			tw,
			"%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			row.Name,
			model.FormatCompactBytes(row.TotalBytes),
			model.FormatCompactBytes(row.RSSBytes()),
			model.FormatCompactBytes(row.CacheBytes()),
			model.FormatCompactBytes(row.ShmemBytes),
			model.FormatCompactBytes(row.SlabBytes),
			explain.Analyze(row).Diagnosis,
		)
	}
	_ = tw.Flush()
}
