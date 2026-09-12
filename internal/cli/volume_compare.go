package cli

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/capability"
	"github.com/danushkastanley/kube-memlens/internal/client"
	"github.com/danushkastanley/kube-memlens/internal/explain"
	"github.com/danushkastanley/kube-memlens/internal/incident"
	"github.com/danushkastanley/kube-memlens/internal/volumeview"
	"github.com/spf13/cobra"
)

func compareVolumeDocuments(w io.Writer, before, after incident.Document, podRef, workloadRef, nodeRef string) error {
	if before.Volume == nil || after.Volume == nil || workloadRef != "" || nodeRef != "" {
		return fmt.Errorf("volume comparison requires two schema-5 Pod incidents")
	}
	b, a := before.Volume, after.Volume
	if podRef != "" && (podRef != b.Pod.Namespace+"/"+b.Pod.PodName || podRef != a.Pod.Namespace+"/"+a.Pod.PodName) {
		return fmt.Errorf("selected Pod is not present in both captures")
	}
	return printVolumeComparison(w, explain.VolumeInput{Pod: b.Pod, Volumes: b.Volumes, Now: b.CapturedAt}, explain.VolumeInput{Pod: a.Pod, Volumes: a.Volumes, Now: a.CapturedAt})
}

func compareLiveVolumes(cmd *cobra.Command, options collectorOptionsProvider, namespace string, args []string) error {
	opts, err := withReadScope(options(), namespace, false)
	if err != nil {
		return err
	}
	if opts.EvidenceMode == capability.Restricted {
		return fmt.Errorf("volume comparison requires the authenticated collector volume profile")
	}
	reader, description, err := client.NewSnapshotReader(cmd.Context(), opts)
	if err != nil {
		return collectorUnavailableError(opts, description, err)
	}
	volumeReader, ok := reader.(client.PodVolumeReader)
	if !ok {
		return fmt.Errorf("volume comparison requires the authenticated Kubernetes API connection")
	}
	var inputs []explain.VolumeInput
	for _, arg := range args {
		name, err := livePodName(arg)
		if err != nil {
			return err
		}
		pod, volumes, err := client.ReadPodVolumeEvidence(cmd.Context(), volumeReader, namespace, name, "")
		if err != nil {
			return err
		}
		inputs = append(inputs, explain.VolumeInput{Pod: pod, Volumes: volumes.Context, Now: time.Now().UTC()})
	}
	return printVolumeComparison(cmd.OutOrStdout(), inputs[0], inputs[1])
}

func printVolumeComparison(w io.Writer, before, after explain.VolumeInput) error {
	result, err := explain.CompareVolumes(before, after)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(w, strings.Join(volumeview.ComparisonLines(before, after, result, 100), "\n"))
	return err
}
