package cli

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/explain"
	"github.com/danushkastanley/kube-memlens/internal/model"
	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/yaml"
)

type topOptions struct {
	LabelSelector string
	FieldSelector string
	SortBy        string
	Output        string
	NoHeaders     bool
	Watch         bool
	WatchInterval time.Duration
}

type topRow struct {
	Namespace     string             `json:"namespace,omitempty"`
	Name          string             `json:"name"`
	Kind          string             `json:"kind,omitempty"`
	Pod           string             `json:"pod,omitempty"`
	Node          string             `json:"node,omitempty"`
	Count         int                `json:"count,omitempty"`
	TotalBytes    uint64             `json:"totalBytes"`
	LimitBytes    uint64             `json:"limitBytes,omitempty"`
	AnonBytes     uint64             `json:"anonBytes"`
	CacheBytes    uint64             `json:"fileCacheBytes"`
	ShmemBytes    uint64             `json:"shmemBytes"`
	KernelBytes   uint64             `json:"kernelBytes"`
	ResidualBytes uint64             `json:"residualBytes"`
	Diagnosis     explain.Diagnosis  `json:"diagnosis"`
	Confidence    explain.Confidence `json:"confidence"`
	LargestName   string             `json:"largestPodName,omitempty"`
	LargestBytes  uint64             `json:"largestPodBytes,omitempty"`
}

func addTopFlags(cmd *cobra.Command, opts *topOptions, selectors bool) {
	if selectors {
		cmd.Flags().StringVarP(&opts.LabelSelector, "selector", "l", "", "Pod label selector")
	}
	cmd.Flags().StringVar(&opts.FieldSelector, "field-selector", "", "field selector for metadata.name, metadata.namespace, spec.nodeName, status.phase, or kind")
	cmd.Flags().StringVar(&opts.SortBy, "sort-by", "total", "sort by total, rss, cache, shmem, kernel, residual, name, namespace, or diagnosis")
	cmd.Flags().StringVarP(&opts.Output, "output", "o", "table", "output format: table, json, yaml, or csv")
	cmd.Flags().BoolVar(&opts.NoHeaders, "no-headers", false, "omit table or CSV headers")
	cmd.Flags().BoolVarP(&opts.Watch, "watch", "w", false, "refresh the table until interrupted")
	cmd.Flags().DurationVar(&opts.WatchInterval, "watch-interval", 2*time.Second, "watch refresh interval")
}

func (o topOptions) validate() error {
	switch o.Output {
	case "table", "json", "yaml", "csv":
	default:
		return fmt.Errorf("invalid output %q, want table, json, yaml, or csv", o.Output)
	}
	if o.NoHeaders && o.Output != "table" && o.Output != "csv" {
		return fmt.Errorf("--no-headers is supported only with table or csv output")
	}
	if o.Watch && o.Output != "table" {
		return fmt.Errorf("--watch is supported only with table output")
	}
	if o.WatchInterval < 500*time.Millisecond || o.WatchInterval > time.Minute {
		return fmt.Errorf("--watch-interval must be between 500ms and 1m")
	}
	for _, field := range []string{"total", "rss", "cache", "shmem", "kernel", "residual", "name", "namespace", "diagnosis"} {
		if o.SortBy == field {
			return nil
		}
	}
	return fmt.Errorf("invalid sort field %q", o.SortBy)
}

func watchTop(ctx context.Context, w io.Writer, opts topOptions, render func() error) error {
	if err := render(); err != nil {
		return err
	}
	if !opts.Watch {
		return nil
	}
	ticker := time.NewTicker(opts.WatchInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			fmt.Fprint(w, "\x1b[H\x1b[2J")
			if err := render(); err != nil {
				return err
			}
		}
	}
}

func parseSelectors(labelSelector, fieldSelector string) (labels.Selector, fields.Selector, error) {
	labelSet, err := labels.Parse(labelSelector)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid label selector: %w", err)
	}
	fieldSet, err := fields.ParseAndTransformSelector(fieldSelector, func(field, value string) (string, string, error) {
		switch field {
		case "metadata.name", "metadata.namespace", "spec.nodeName", "status.phase", "kind", "pod":
			return field, value, nil
		default:
			return "", "", fmt.Errorf("unsupported field %q", field)
		}
	})
	if err != nil {
		return nil, nil, fmt.Errorf("invalid field selector: %w", err)
	}
	return labelSet, fieldSet, nil
}

func sortRows(rows []topRow, field string) {
	sort.SliceStable(rows, func(i, j int) bool {
		left, right := rows[i], rows[j]
		switch field {
		case "name":
			return left.Name < right.Name
		case "namespace":
			if left.Namespace != right.Namespace {
				return left.Namespace < right.Namespace
			}
			return left.Name < right.Name
		case "diagnosis":
			if left.Diagnosis != right.Diagnosis {
				return left.Diagnosis < right.Diagnosis
			}
			return left.TotalBytes > right.TotalBytes
		case "rss":
			return left.AnonBytes > right.AnonBytes
		case "cache":
			return left.CacheBytes > right.CacheBytes
		case "shmem":
			return left.ShmemBytes > right.ShmemBytes
		case "kernel":
			return left.KernelBytes > right.KernelBytes
		case "residual":
			return left.ResidualBytes > right.ResidualBytes
		default:
			return left.TotalBytes > right.TotalBytes
		}
	})
}

func writeTopRows(w io.Writer, rows []topRow, opts topOptions, table func(io.Writer, []topRow, bool)) error {
	sortRows(rows, opts.SortBy)
	switch opts.Output {
	case "table":
		table(w, rows, opts.NoHeaders)
		return nil
	case "json":
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(rows)
	case "yaml":
		body, err := json.Marshal(rows)
		if err != nil {
			return err
		}
		body, err = yaml.JSONToYAML(body)
		if err != nil {
			return err
		}
		_, err = w.Write(body)
		return err
	case "csv":
		writer := csv.NewWriter(w)
		if !opts.NoHeaders {
			_ = writer.Write([]string{"namespace", "kind", "name", "pod", "node", "count", "total_bytes", "limit_bytes", "anon_bytes", "file_cache_bytes", "shmem_bytes", "kernel_bytes", "residual_bytes", "diagnosis", "confidence"})
		}
		for _, row := range rows {
			_ = writer.Write([]string{row.Namespace, row.Kind, row.Name, row.Pod, row.Node, strconv.Itoa(row.Count), strconv.FormatUint(row.TotalBytes, 10), strconv.FormatUint(row.LimitBytes, 10), strconv.FormatUint(row.AnonBytes, 10), strconv.FormatUint(row.CacheBytes, 10), strconv.FormatUint(row.ShmemBytes, 10), strconv.FormatUint(row.KernelBytes, 10), strconv.FormatUint(row.ResidualBytes, 10), string(row.Diagnosis), string(row.Confidence)})
		}
		writer.Flush()
		return writer.Error()
	default:
		return fmt.Errorf("unsupported output %q", opts.Output)
	}
}

func rowForMemory(namespace, kind, name, pod, node string, count int, memory model.MemoryBreakdown) topRow {
	result := explain.Analyze(memory)
	return topRow{
		Namespace: namespace, Kind: kind, Name: name, Pod: pod, Node: node, Count: count,
		TotalBytes: memory.TotalBytes, AnonBytes: memory.RSSBytes(), CacheBytes: memory.CacheBytes(),
		ShmemBytes: memory.ShmemBytes, KernelBytes: memory.KernelBytes, ResidualBytes: memory.ResidualBytes(),
		Diagnosis: result.Diagnosis, Confidence: result.Confidence,
	}
}

func fieldValues(name, namespace, node, phase, kind, pod string) fields.Set {
	return fields.Set{"metadata.name": name, "metadata.namespace": namespace, "spec.nodeName": node, "status.phase": phase, "kind": kind, "pod": pod}
}

func labelMatches(selector labels.Selector, values map[string]string) bool {
	return selector.Matches(labels.Set(values))
}
