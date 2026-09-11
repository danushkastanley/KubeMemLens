package cli

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/capability"
	"github.com/danushkastanley/kube-memlens/internal/client"
	"github.com/danushkastanley/kube-memlens/internal/observation"
	"github.com/danushkastanley/kube-memlens/internal/observationview"
	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"
)

type restrictedReportKind string

const (
	restrictedExplain   restrictedReportKind = "explanation"
	restrictedRecommend restrictedReportKind = "recommendation"
)

// Restricted documents use distinct schema versions so a deep-only consumer
// rejects them rather than interpreting missing composition as numeric zero.
type restrictedReport struct {
	SchemaVersion     int                        `json:"schemaVersion"`
	Mode              capability.Mode            `json:"mode"`
	GeneratedAt       time.Time                  `json:"generatedAt"`
	Target            explanationTarget          `json:"target"`
	Observation       observationview.Row        `json:"observation"`
	Children          []observationview.Row      `json:"children,omitempty"`
	Sources           []observation.SourceReport `json:"sources"`
	Unavailable       []capability.QueryState    `json:"unavailable"`
	Details           []string                   `json:"details"`
	Recommendations   []string                   `json:"recommendations"`
	AutomaticMutation bool                       `json:"automaticMutation"`
}

func runRestrictedReport(cmd *cobra.Command, session client.EvidenceSession, target explanationTarget, scope capability.Scope, output string, kind restrictedReportKind) error {
	batch, err := session.Observations.Current(cmd.Context())
	if err != nil {
		return err
	}
	rows := observationview.Rows(batch)
	for _, row := range rows {
		if row.Scope != scope || row.Namespace != target.Namespace || row.Name != target.Name || !strings.EqualFold(row.Kind, target.Kind) {
			continue
		}
		now := time.Now().UTC()
		document := restrictedReport{SchemaVersion: api.RestrictedExplanationSchemaVersion, Mode: capability.Restricted, GeneratedAt: now, Target: explanationTarget{Kind: row.Kind, Namespace: row.Namespace, Name: row.Name},
			Observation: row, Sources: batch.Sources, Details: observationview.Detail(row, now), Recommendations: observationview.Recommendations(row)}
		if kind == restrictedRecommend {
			document.SchemaVersion = api.RestrictedRecommendationSchemaVersion
		}
		for _, query := range session.Plan.Queries {
			if query.Availability != capability.Available {
				document.Unavailable = append(document.Unavailable, query)
			}
		}
		for _, child := range rows {
			if scope == capability.PodScope && child.Scope == capability.ContainerScope && child.Namespace == row.Namespace && child.PodName == row.Name {
				document.Children = append(document.Children, child)
			}
			if scope == capability.WorkloadScope && child.Scope == capability.PodScope && child.Namespace == row.Namespace && child.WorkloadKind == row.Kind && child.WorkloadName == row.Name {
				document.Children = append(document.Children, child)
			}
		}
		if output == "text" {
			fmt.Fprintln(cmd.OutOrStdout(), "Mode: restricted / Kubernetes APIs")
			lines := document.Details
			if kind == restrictedRecommend {
				lines = append(observationview.Summary(row, now), append([]string{"", "Read-only recommendations:"}, document.Recommendations...)...)
			}
			_, err := fmt.Fprintln(cmd.OutOrStdout(), strings.Join(lines, "\n"))
			return err
		}
		data, err := json.MarshalIndent(document, "", "  ")
		if err != nil {
			return err
		}
		if output == "yaml" {
			data, err = yaml.JSONToYAML(data)
			if err != nil {
				return err
			}
		}
		_, err = cmd.OutOrStdout().Write(append(data, '\n'))
		return err
	}
	return fmt.Errorf("the requested object was not found in the authorised current observations")
}
