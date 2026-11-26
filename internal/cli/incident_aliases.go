package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/danushkastanley/kube-memlens/internal/api"
)

// maxListedIncidentRefs bounds the names a selector error lists, so a large
// capture cannot flood the terminal.
const maxListedIncidentRefs = 10

const redactedAliasComparison = "Redacted captures number aliases separately in each file. An alias names the same Pod or workload in both files only when both captures held the same Pods; capture one Pod with --namespace and --pod to keep its alias."

// printCaptureAliasNote tells the operator how to address a redacted capture.
// The file holds no mapping back to real names, so a single-Pod capture names
// its Pod's alias here and a wider capture points to replay, which lists them.
func printCaptureAliasNote(w io.Writer, bundle api.IncidentBundle, podName string) {
	if podName != "" && len(bundle.Pods) == 1 {
		pod := bundle.Pods[0]
		fmt.Fprintf(w, "Names in the file are capture-local aliases. Replay or compare this Pod as --pod %s/%s.\n", pod.Namespace, pod.PodName)
		return
	}
	fmt.Fprintln(w, "Names in the file are capture-local aliases. Replay the file to list them.")
}

// incidentSelectorError keeps the existing message for a sensitive capture.
// For a redacted capture it explains that names are aliases and lists the
// ones the file holds.
func incidentSelectorError(message string, bundle api.IncidentBundle, noun string, refs []string) error {
	if !bundle.Redacted {
		return fmt.Errorf("%s", message)
	}
	prefix := message + ". Redacted captures use capture-local aliases instead of cluster names"
	if len(refs) == 0 {
		return fmt.Errorf("%s; this file holds no %s", prefix, noun)
	}
	listed := refs[:min(len(refs), maxListedIncidentRefs)]
	text := fmt.Sprintf("%s; %s in this file: %s", prefix, noun, strings.Join(listed, ", "))
	if more := len(refs) - len(listed); more > 0 {
		text += fmt.Sprintf(", and %d more", more)
	}
	return fmt.Errorf("%s", text)
}

func incidentPodSelectorError(message string, bundle api.IncidentBundle) error {
	refs := make([]string, 0, len(bundle.Pods))
	for _, pod := range bundle.Pods {
		refs = append(refs, pod.Namespace+"/"+pod.PodName)
	}
	return incidentSelectorError(message, bundle, "Pods", refs)
}

func incidentWorkloadSelectorError(message string, bundle api.IncidentBundle) error {
	refs := []string{}
	seen := map[string]bool{}
	for _, pod := range bundle.Pods {
		ref := pod.Namespace + "/" + pod.Context.WorkloadKind + "/" + pod.Context.WorkloadName
		if pod.Context.WorkloadName == "" || seen[ref] {
			continue
		}
		seen[ref] = true
		refs = append(refs, ref)
	}
	return incidentSelectorError(message, bundle, "workloads", refs)
}
