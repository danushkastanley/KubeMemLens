package cli

import (
	"fmt"
	"io"
	"runtime"
	"strings"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/buildinfo"
	"github.com/danushkastanley/kube-memlens/internal/capability"
	"github.com/danushkastanley/kube-memlens/internal/client"
	"github.com/danushkastanley/kube-memlens/internal/incident"
	"github.com/danushkastanley/kube-memlens/internal/observation"
	"github.com/danushkastanley/kube-memlens/internal/observationview"
	"github.com/spf13/cobra"
)

func captureRestricted(cmd *cobra.Command, session client.EvidenceSession, namespace, pod, output string, schema int, history, sensitive, force bool) error {
	if schema != 0 && schema != incident.RestrictedSchemaVersion {
		return fmt.Errorf("restricted evidence requires incident schema 3; downgrade to cgroup schemas 1/2 is unavailable")
	}
	if history {
		return fmt.Errorf("history requires deep evidence; restricted capture cannot include history")
	}
	batch, err := session.Observations.Current(cmd.Context())
	if err != nil {
		return err
	}
	version := buildinfo.Current(runtime.Version(), runtime.GOOS, runtime.GOARCH).String()
	bundle, err := incident.NewRestricted(batch, namespace, pod, version, time.Now(), sensitive)
	if err != nil {
		return err
	}
	if err := incident.WriteRestricted(cmd.OutOrStdout(), output, force, bundle); err != nil {
		return err
	}
	if output != "-" {
		_, err = fmt.Fprintf(cmd.OutOrStdout(), "Wrote %s (%d Pods; mode=restricted; schema=3; redacted=%t; completeness=%s)\n", output, len(bundle.Observations.Pods), bundle.Redacted, bundle.Observations.Completeness)
	}
	return err
}

func replayRestricted(w io.Writer, bundle incident.RestrictedBundle, podRef string) error {
	lines := []string{"Mode: restricted / Kubernetes APIs", "Captured: " + bundle.CapturedAt.Format(time.RFC3339Nano), "Completeness: " + string(bundle.Observations.Completeness)}
	lines = append(lines, bundle.Observations.Caveats...)
	rows := observationview.Rows(bundle.Observations)
	observationview.Sort(rows, observationview.ByName)
	if podRef == "" && len(bundle.Observations.Pods) == 1 {
		pod := bundle.Observations.Pods[0]
		podRef = pod.Namespace + "/" + pod.Name
	}
	if podRef != "" {
		row, ok := restrictedIncidentRow(bundle.Observations, podRef, "")
		if !ok {
			return fmt.Errorf("selected Pod was not found in restricted incident")
		}
		lines = append(lines, observationview.Detail(row, bundle.CapturedAt)...)
		_, err := fmt.Fprintln(w, strings.Join(lines, "\n"))
		return err
	}
	for _, row := range rows {
		if row.Scope == capability.PodScope {
			lines = append(lines, "", strings.Join(observationview.Summary(row, bundle.CapturedAt), "\n"))
		}
	}
	_, err := fmt.Fprintln(w, strings.Join(lines, "\n"))
	return err
}

func restrictedIncidentRow(batch observation.Batch, podRef, workloadRef string) (observationview.Row, bool) {
	for _, row := range observationview.Rows(batch) {
		if podRef != "" && row.Scope == capability.PodScope && row.Namespace+"/"+row.Name == podRef {
			return row, true
		}
		if workloadRef != "" && row.Scope == capability.WorkloadScope && strings.EqualFold(row.Namespace+"/"+row.Kind+"/"+row.Name, workloadRef) {
			return row, true
		}
	}
	return observationview.Row{}, false
}

func compareRestrictedDocuments(w io.Writer, before, after incident.Document, podRef, workloadRef string) error {
	if before.Restricted == nil || after.Restricted == nil {
		return fmt.Errorf("cannot compare Kubernetes working set with cgroup charge")
	}
	left, ok := restrictedIncidentRow(before.Restricted.Observations, podRef, workloadRef)
	if !ok {
		return fmt.Errorf("selected entity was not found in before restricted incident")
	}
	right, ok := restrictedIncidentRow(after.Restricted.Observations, podRef, workloadRef)
	if !ok {
		return fmt.Errorf("selected entity was not found in after restricted incident")
	}
	lines, err := observationview.Compare(left, right, before.Restricted.CapturedAt, after.Restricted.CapturedAt)
	if err != nil {
		return err
	}
	for _, capture := range []struct {
		label  string
		bundle *incident.RestrictedBundle
	}{{"Before", before.Restricted}, {"After", after.Restricted}} {
		lines = append(lines, capture.label+" capture completeness: "+string(capture.bundle.Observations.Completeness))
		for _, caveat := range capture.bundle.Observations.Caveats {
			lines = append(lines, capture.label+" capture caveat: "+caveat)
		}
	}
	_, err = fmt.Fprintln(w, strings.Join(lines, "\n"))
	return err
}

func compareRestrictedLive(cmd *cobra.Command, session client.EvidenceSession, namespace string, args []string) error {
	leftName, err := livePodName(args[0])
	if err != nil {
		return err
	}
	rightName, err := livePodName(args[1])
	if err != nil {
		return err
	}
	batch, err := session.Observations.Current(cmd.Context())
	if err != nil {
		return err
	}
	left, ok := restrictedIncidentRow(batch, namespace+"/"+leftName, "")
	if !ok {
		return fmt.Errorf("first Pod was not found in current observations")
	}
	right, ok := restrictedIncidentRow(batch, namespace+"/"+rightName, "")
	if !ok {
		return fmt.Errorf("second Pod was not found in current observations")
	}
	lines, err := observationview.Compare(left, right, batch.ReceivedAt, batch.ReceivedAt)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(cmd.OutOrStdout(), strings.Join(lines, "\n"))
	return err
}
