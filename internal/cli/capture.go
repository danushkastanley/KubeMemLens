package cli

import (
	"fmt"
	"io"
	"runtime"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/buildinfo"
	"github.com/danushkastanley/kube-memlens/internal/capability"
	"github.com/danushkastanley/kube-memlens/internal/incident"
	"github.com/spf13/cobra"
)

const maxCaptureHistoryPods = 100

func newCaptureCommand(collectorOptions collectorOptionsProvider) *cobra.Command {
	var output, namespace, podName string
	var schemaVersion int
	var includeHistory, includeSensitive, force bool
	cmd := &cobra.Command{
		Use:   "capture",
		Short: "Write a redacted incident bundle for offline replay",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			opts, err := withReadScope(collectorOptions(), namespace, namespace == "")
			if err != nil {
				return err
			}
			session, err := currentSession(cmd.Context(), opts)
			reader, description := session.Reader, session.Description
			if err != nil {
				return collectorUnavailableError(opts, description, err)
			}
			if session.Plan.Mode == capability.Restricted {
				return captureRestricted(cmd, session, namespace, podName, output, schemaVersion, includeHistory, includeSensitive, force)
			}
			if schemaVersion == incident.RestrictedSchemaVersion {
				return fmt.Errorf("incident schema 3 requires restricted mode")
			}
			pods, err := reader.Pods(cmd.Context())
			if err != nil {
				return collectorUnavailableError(opts, description, err)
			}
			pods = selectCapturePods(pods, namespace, podName)
			if podName != "" && len(pods) == 0 {
				return fmt.Errorf("Pod %s/%s was not found in current collector snapshots", namespace, podName)
			}
			var nodes []api.NodeSnapshotStatus
			var reliability api.CollectorReliability
			if namespace == "" {
				nodes, err = reader.Nodes(cmd.Context())
				if err != nil {
					return collectorUnavailableError(opts, description, err)
				}
				debug, debugErr := reader.DebugStore(cmd.Context())
				if debugErr != nil {
					return collectorUnavailableError(opts, description, debugErr)
				}
				reliability = debug.Reliability
			} else {
				reliability = captureReliabilityFromPods(pods)
			}
			bundle := api.IncidentBundle{
				SchemaVersion: api.IncidentSchema(pods),
				CapturedAt:    time.Now().UTC(),
				ToolVersion:   buildinfo.Current(runtime.Version(), runtime.GOOS, runtime.GOARCH).String(),
				Redacted:      !includeSensitive,
				Pods:          pods,
				Nodes:         nodes,
				Reliability:   &reliability,
			}
			if namespace != "" {
				bundle.Partial = true
				bundle.Caveats = []string{"Cluster node summaries are omitted from a namespace-scoped capture."}
			}
			if reliability.State != api.CollectorReady {
				bundle.Partial = true
				bundle.Caveats = append(bundle.Caveats, "Collector evidence state at capture: "+string(reliability.State)+".")
			}
			if includeHistory {
				if len(pods) > maxCaptureHistoryPods {
					return fmt.Errorf("history capture is limited to %d Pods; use --namespace or --pod to narrow the bundle", maxCaptureHistoryPods)
				}
				for _, pod := range pods {
					history, historyErr := reader.PodHistory(cmd.Context(), pod.Namespace, pod.PodName)
					if historyErr != nil {
						return collectorUnavailableError(opts, description, historyErr)
					}
					bundle.Histories = append(bundle.Histories, history...)
				}
			}
			switch schemaVersion {
			case api.LegacySchemaVersion:
				bundle = api.LegacyIncident(bundle)
			case api.CurrentIncidentSchemaVersion:
				bundle.SchemaVersion = api.CurrentIncidentSchemaVersion
			}
			if bundle.Redacted {
				redactIncident(&bundle)
			}
			if err := writeIncidentBundle(cmd.OutOrStdout(), output, force, bundle); err != nil {
				return err
			}
			if output != "-" {
				fmt.Fprintf(cmd.OutOrStdout(), "Wrote %s (%d Pods, %d history series; redacted=%t; partial=%t)\n", output, len(bundle.Pods), len(bundle.Histories), bundle.Redacted, bundle.Partial)
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&output, "output", "o", "kube-memlens-incident.json", "output file, or - for stdout")
	cmd.Flags().StringVarP(&namespace, "namespace", "n", "", "capture only this namespace")
	cmd.Flags().StringVar(&podName, "pod", "", "capture only this Pod; requires --namespace")
	cmd.Flags().BoolVar(&includeHistory, "include-history", false, "include bounded recent history for captured Pods")
	cmd.Flags().BoolVar(&includeSensitive, "include-sensitive", false, "include Pod UIDs, container IDs, and cgroup paths")
	cmd.Flags().BoolVar(&force, "force", false, "replace an existing output file")
	cmd.Flags().IntVar(&schemaVersion, "schema-version", 0, "incident schema: 0 selects automatically; 1 omits resource/resize context for older readers; 2 retains it; 3 records restricted evidence")
	cmd.PreRunE = func(_ *cobra.Command, _ []string) error {
		if schemaVersion < 0 || schemaVersion > incident.RestrictedSchemaVersion {
			return fmt.Errorf("--schema-version must be 0, 1, 2 or %d", incident.RestrictedSchemaVersion)
		}
		if podName != "" && namespace == "" {
			return fmt.Errorf("--pod requires --namespace")
		}
		if output == "" {
			return fmt.Errorf("--output must not be empty")
		}
		return nil
	}
	return cmd
}

func captureReliabilityFromPods(pods []api.PodSnapshot) api.CollectorReliability {
	result := api.CollectorReliability{State: api.CollectorRebuilding, Completeness: api.EvidencePartial}
	fresh, stale := 0, 0
	partial := false
	for _, pod := range pods {
		if pod.Freshness == api.EvidenceFreshnessStale {
			stale++
		} else {
			fresh++
		}
		partial = partial || pod.Completeness == api.EvidencePartial
	}
	if fresh > 0 && stale == 0 && !partial {
		result.State, result.Completeness = api.CollectorReady, api.EvidenceComplete
	}
	if fresh > 0 && (stale > 0 || partial) {
		result.State = api.CollectorDegraded
	}
	if fresh == 0 && stale > 0 {
		result.State = api.CollectorStale
	}
	return result
}

func selectCapturePods(pods []api.PodSnapshot, namespace, podName string) []api.PodSnapshot {
	selected := make([]api.PodSnapshot, 0, len(pods))
	for _, pod := range pods {
		if namespace != "" && pod.Namespace != namespace {
			continue
		}
		if podName != "" && pod.PodName != podName {
			continue
		}
		selected = append(selected, pod)
	}
	return selected
}

func redactIncident(bundle *api.IncidentBundle) {
	incident.Redact(bundle)
}

func writeIncidentBundle(stdout io.Writer, output string, force bool, bundle api.IncidentBundle) error {
	return incident.Write(stdout, output, force, bundle)
}
