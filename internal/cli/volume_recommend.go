package cli

import (
	"fmt"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/capability"
	"github.com/danushkastanley/kube-memlens/internal/client"
	"github.com/danushkastanley/kube-memlens/internal/explain"
	"github.com/danushkastanley/kube-memlens/internal/recommend"
	"github.com/danushkastanley/kube-memlens/internal/volumeview"
	"github.com/spf13/cobra"
)

type volumeRecommendationDocument struct {
	SchemaVersion     int                        `json:"schemaVersion"`
	GeneratedAt       time.Time                  `json:"generatedAt"`
	Target            explanationTarget          `json:"target"`
	Analysis          any                        `json:"analysis"`
	Recommendations   []recommend.Recommendation `json:"recommendations"`
	Commands          []string                   `json:"commands"`
	AutomaticMutation bool                       `json:"automaticMutation"`
}

func runVolumeRecommendations(cmd *cobra.Command, options collectorOptionsProvider, namespace, kind, name, output string) error {
	if err := validateRecommendationOutput(output); err != nil {
		return err
	}
	opts, err := withReadScope(options(), namespace, false)
	if err != nil {
		return err
	}
	if opts.EvidenceMode == capability.Restricted {
		return fmt.Errorf("volume recommendations require the authenticated collector volume profile")
	}
	reader, description, err := client.NewSnapshotReader(cmd.Context(), opts)
	if err != nil {
		return collectorUnavailableError(opts, description, err)
	}
	doc := volumeRecommendationDocument{SchemaVersion: 3, Target: explanationTarget{Kind: kind, Namespace: namespace, Name: name}}
	if kind == "Pod" {
		r, ok := reader.(client.PodVolumeReader)
		if !ok {
			return fmt.Errorf("volume recommendations require the authenticated Kubernetes API connection")
		}
		pod, volumes, err := client.ReadPodVolumeEvidence(cmd.Context(), r, namespace, name, "")
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		analysis := explain.AnalyzeVolumes(explain.VolumeInput{Pod: pod, Volumes: volumes.Context, Now: now})
		memory := explain.AnalyzePodAt(pod, now)
		doc.GeneratedAt, doc.Analysis = now, analysis
		doc.Recommendations = recommend.ForVolumeEvidence(&memory, []explain.VolumeResult{analysis})
		doc.Recommendations = append(doc.Recommendations, recommend.ForPodMemoryQoS([]api.PodSnapshot{pod})...)
		doc.Commands = volumeview.Commands(volumes.Context)
	} else {
		r, ok := reader.(client.WorkloadVolumeReader)
		if !ok {
			return fmt.Errorf("workload volume recommendations require the authenticated Kubernetes API connection")
		}
		value, err := r.WorkloadVolumes(cmd.Context(), namespace, kind, name)
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		analysis := explain.AnalyzeWorkloadVolumes(value, now)
		var findings []explain.VolumeResult
		for _, pod := range analysis.Pods {
			findings = append(findings, pod.Analysis)
		}
		var memory *explain.Result
		if len(value.Workload.Pods) > 0 {
			result := explain.AnalyzeWorkload(value.Workload)
			memory = &result
		}
		doc.GeneratedAt, doc.Analysis = now, analysis
		doc.Recommendations = recommend.ForVolumeEvidence(memory, findings)
		doc.Recommendations = append(doc.Recommendations, recommend.ForPodMemoryQoS(value.Workload.Pods)...)
		for _, pod := range value.PodVolumes {
			doc.Commands = append(doc.Commands, "kubectl memlens volumes pod "+volumeview.Quote(pod.Name)+" -n "+volumeview.Quote(pod.Namespace))
		}
	}
	if output != "text" {
		return writeNodeDocument(cmd.OutOrStdout(), output, doc)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Volume-aware recommendations: %s/%s/%s\nAutomatic mutation: disabled\n", kind, namespace, name)
	for _, item := range doc.Recommendations {
		fmt.Fprintf(cmd.OutOrStdout(), "\n%s\n%s\n", item.Action, item.Rationale)
		for _, condition := range item.Conditions {
			fmt.Fprintln(cmd.OutOrStdout(), "- "+condition)
		}
	}
	for _, command := range doc.Commands {
		fmt.Fprintln(cmd.OutOrStdout(), command)
	}
	return nil
}
