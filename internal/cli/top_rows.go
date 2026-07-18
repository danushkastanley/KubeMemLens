package cli

import (
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/explain"
	"github.com/danushkastanley/kube-memlens/internal/model"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
)

func podRows(pods []api.PodSnapshot, labelSelector labels.Selector, fieldSelector fields.Selector) []topRow {
	rows := []topRow{}
	for _, pod := range pods {
		if !labelMatches(labelSelector, pod.Context.Labels) || !fieldSelector.Matches(fieldValues(pod.PodName, pod.Namespace, pod.NodeName, pod.Context.Phase, "Pod", pod.PodName)) {
			continue
		}
		row := rowForMemory(pod.Namespace, "Pod", pod.PodName, "", pod.NodeName, 0, pod.Memory)
		result := explain.AnalyzePod(pod)
		row.Diagnosis, row.Confidence = result.Diagnosis, result.Confidence
		row.LimitBytes = finiteLimit(pod.Memory)
		rows = append(rows, row)
	}
	return rows
}

func containerRows(containers []api.ContainerSnapshot, labelSelector labels.Selector, fieldSelector fields.Selector) []topRow {
	rows := []topRow{}
	for _, container := range mappedContainers(containers) {
		if !labelMatches(labelSelector, container.Context.Labels) || !fieldSelector.Matches(fieldValues(container.ContainerName, container.Namespace, container.NodeName, container.Context.PodPhase, "Container", container.PodName)) {
			continue
		}
		row := rowForMemory(container.Namespace, "Container", container.ContainerName, container.PodName, container.NodeName, 0, container.Memory)
		row.LimitBytes = finiteLimit(container.Memory)
		rows = append(rows, row)
	}
	return rows
}

func workloadRows(workloads []api.WorkloadSnapshot, labelSelector labels.Selector, fieldSelector fields.Selector) []topRow {
	rows := []topRow{}
	for _, workload := range workloads {
		if !workloadLabelMatches(workload, labelSelector) || !fieldSelector.Matches(fieldValues(workload.Name, workload.Namespace, "", "", workload.Kind, "")) {
			continue
		}
		row := rowForMemory(workload.Namespace, workload.Kind, workload.Name, "", "", workload.PodCount, workload.Memory)
		row.LargestName, row.LargestBytes = workload.LargestPodName, workload.LargestPodBytes
		rows = append(rows, row)
	}
	return rows
}

func workloadLabelMatches(workload api.WorkloadSnapshot, selector labels.Selector) bool {
	for _, pod := range workload.Pods {
		if labelMatches(selector, pod.Context.Labels) {
			return true
		}
	}
	return len(workload.Pods) == 0 && selector.Empty()
}

func namespaceRows(namespaces []api.NamespaceSnapshot, fieldSelector fields.Selector) []topRow {
	rows := []topRow{}
	for _, namespace := range namespaces {
		if !fieldSelector.Matches(fieldValues(namespace.Namespace, namespace.Namespace, "", "", "Namespace", "")) {
			continue
		}
		rows = append(rows, rowForMemory(namespace.Namespace, "Namespace", namespace.Namespace, "", "", namespace.PodCount, namespace.Memory))
	}
	return rows
}

func finiteLimit(memory model.MemoryBreakdown) uint64 {
	if memory.MaxKnown && !memory.MaxUnlimited {
		return memory.MaxBytes
	}
	return 0
}

func printPodRows(w io.Writer, rows []topRow, noHeaders bool) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	if !noHeaders {
		fmt.Fprintln(tw, "NAMESPACE\tPOD\tNODE\tTOTAL\tLIMIT\tRSS\tCACHE\tSHMEM\tOTHER\tDIAGNOSIS\tCONFIDENCE")
	}
	for _, row := range rows {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n", row.Namespace, row.Name, row.Node, compact(row.TotalBytes), compactLimit(row.LimitBytes), compact(row.AnonBytes), compact(row.CacheBytes), compact(row.ShmemBytes), compact(row.ResidualBytes), row.Diagnosis, row.Confidence)
	}
	_ = tw.Flush()
}

func printContainerRows(w io.Writer, rows []topRow, noHeaders bool) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	if !noHeaders {
		fmt.Fprintln(tw, "NAMESPACE\tPOD\tCONTAINER\tNODE\tTOTAL\tLIMIT\tRSS\tCACHE\tSHMEM\tOTHER\tDIAGNOSIS\tCONFIDENCE")
	}
	for _, row := range rows {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n", row.Namespace, row.Pod, row.Name, row.Node, compact(row.TotalBytes), compactLimit(row.LimitBytes), compact(row.AnonBytes), compact(row.CacheBytes), compact(row.ShmemBytes), compact(row.ResidualBytes), row.Diagnosis, row.Confidence)
	}
	_ = tw.Flush()
}

func printWorkloadRows(w io.Writer, rows []topRow, noHeaders bool) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	if !noHeaders {
		fmt.Fprintln(tw, "NAMESPACE\tKIND\tWORKLOAD\tPODS\tTOTAL\tRSS\tCACHE\tSHMEM\tOTHER\tLARGEST POD\tMAX POD\tDIAGNOSIS\tCONFIDENCE")
	}
	for _, row := range rows {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n", row.Namespace, row.Kind, row.Name, row.Count, compact(row.TotalBytes), compact(row.AnonBytes), compact(row.CacheBytes), compact(row.ShmemBytes), compact(row.ResidualBytes), row.LargestName, compact(row.LargestBytes), row.Diagnosis, row.Confidence)
	}
	_ = tw.Flush()
}

func printNamespaceRows(w io.Writer, rows []topRow, noHeaders bool) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	if !noHeaders {
		fmt.Fprintln(tw, "NAMESPACE\tPODS\tTOTAL\tRSS\tCACHE\tSHMEM\tOTHER\tDIAGNOSIS\tCONFIDENCE")
	}
	for _, row := range rows {
		fmt.Fprintf(tw, "%s\t%d\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n", row.Namespace, row.Count, compact(row.TotalBytes), compact(row.AnonBytes), compact(row.CacheBytes), compact(row.ShmemBytes), compact(row.ResidualBytes), row.Diagnosis, row.Confidence)
	}
	_ = tw.Flush()
}

func compact(bytes uint64) string { return model.FormatCompactBytes(bytes) }

func compactLimit(bytes uint64) string {
	if bytes == 0 {
		return "unknown"
	}
	return compact(bytes)
}
