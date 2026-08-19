package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/client"
	"github.com/danushkastanley/kube-memlens/internal/explain"
	"github.com/danushkastanley/kube-memlens/internal/recommend"
	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"
)

type recommendationDocument struct {
	SchemaVersion     int                        `json:"schemaVersion"`
	GeneratedAt       time.Time                  `json:"generatedAt"`
	Target            explanationTarget          `json:"target"`
	Finding           findingEvidence            `json:"finding"`
	Recommendations   []recommend.Recommendation `json:"recommendations"`
	AutomaticMutation bool                       `json:"automaticMutation"`
}

func newRecommendCommand(collectorOptions collectorOptionsProvider) *cobra.Command {
	cmd := &cobra.Command{Use: "recommend", Short: "Export composition-aware, read-only recommendations"}
	cmd.AddCommand(newRecommendPodCommand(collectorOptions), newRecommendWorkloadCommand(collectorOptions))
	return cmd
}

func newRecommendPodCommand(collectorOptions collectorOptionsProvider) *cobra.Command {
	var namespace, output string
	cmd := &cobra.Command{
		Use: "pod <pod-name>", Short: "Recommend next investigation steps for one Pod", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateRecommendationOutput(output); err != nil {
				return err
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
			pod, ok := findPod(pods, namespace, args[0])
			if !ok {
				return fmt.Errorf("Pod %s/%s was not found in current collector snapshots", namespace, args[0])
			}
			finding := explain.AnalyzePod(pod)
			document := recommendationOutput(explanationTarget{Kind: "Pod", Namespace: namespace, Name: args[0]}, finding)
			return writeRecommendationDocument(cmd.OutOrStdout(), output, document)
		},
	}
	cmd.Flags().StringVarP(&namespace, "namespace", "n", "default", "Kubernetes namespace")
	cmd.Flags().StringVarP(&output, "output", "o", "text", "output format: text, json, or yaml")
	return cmd
}

func newRecommendWorkloadCommand(collectorOptions collectorOptionsProvider) *cobra.Command {
	var namespace, output string
	cmd := &cobra.Command{
		Use: "workload <kind>/<name>", Short: "Recommend next investigation steps for a workload", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateRecommendationOutput(output); err != nil {
				return err
			}
			parts := strings.Split(args[0], "/")
			if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
				return fmt.Errorf("workload must be written as <kind>/<name>")
			}
			opts := collectorOptions()
			reader, description, err := client.NewSnapshotReader(cmd.Context(), opts)
			if err != nil {
				return collectorUnavailableError(opts, description, err)
			}
			workloads, err := reader.Workloads(cmd.Context())
			if err != nil {
				return collectorUnavailableError(opts, description, err)
			}
			for _, workload := range workloads {
				if workload.Namespace == namespace && workload.Name == parts[1] && strings.EqualFold(workload.Kind, parts[0]) {
					finding := explain.AnalyzeWorkload(workload)
					document := recommendationOutput(explanationTarget{Kind: workload.Kind, Namespace: namespace, Name: workload.Name}, finding)
					return writeRecommendationDocument(cmd.OutOrStdout(), output, document)
				}
			}
			return fmt.Errorf("workload %s/%s was not found in current collector snapshots", parts[0], parts[1])
		},
	}
	cmd.Flags().StringVarP(&namespace, "namespace", "n", "default", "Kubernetes namespace")
	cmd.Flags().StringVarP(&output, "output", "o", "text", "output format: text, json, or yaml")
	return cmd
}

func recommendationOutput(target explanationTarget, finding explain.Result) recommendationDocument {
	return recommendationDocument{
		SchemaVersion: api.CurrentExplanationSchemaVersion, GeneratedAt: time.Now().UTC(), Target: target,
		Finding: findingOutput(finding), Recommendations: recommend.ForFinding(finding), AutomaticMutation: false,
	}
}

func validateRecommendationOutput(output string) error {
	if output != "text" && output != "json" && output != "yaml" {
		return fmt.Errorf("invalid output %q, want text, json, or yaml", output)
	}
	return nil
}

func writeRecommendationDocument(w io.Writer, output string, document recommendationDocument) error {
	if output == "text" {
		fmt.Fprintf(w, "Recommendation: %s/%s/%s\nDiagnosis: %s (%s confidence)\nAutomatic mutation: disabled\n\n", document.Target.Kind, document.Target.Namespace, document.Target.Name, document.Finding.Diagnosis, document.Finding.Confidence)
		for _, item := range document.Recommendations {
			fmt.Fprintf(w, "%s [%s]\n%s\nWhy: %s\n", item.ID, item.Priority, item.Action, item.Rationale)
			for _, condition := range item.Conditions {
				fmt.Fprintln(w, "- "+condition)
			}
			fmt.Fprintln(w)
		}
		return nil
	}
	body, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return err
	}
	if output == "yaml" {
		body, err = yaml.JSONToYAML(body)
		if err != nil {
			return err
		}
	}
	_, err = fmt.Fprintln(w, string(body))
	return err
}
