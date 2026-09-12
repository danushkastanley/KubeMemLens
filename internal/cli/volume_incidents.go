package cli

import (
	"bytes"
	"fmt"
	"io"
	"runtime"
	"strings"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/buildinfo"
	"github.com/danushkastanley/kube-memlens/internal/capability"
	"github.com/danushkastanley/kube-memlens/internal/client"
	"github.com/danushkastanley/kube-memlens/internal/explain"
	"github.com/danushkastanley/kube-memlens/internal/incident"
	"github.com/danushkastanley/kube-memlens/internal/nodeview"
	"github.com/danushkastanley/kube-memlens/internal/volumeview"
	"github.com/spf13/cobra"
)

func captureVolume(cmd *cobra.Command, options collectorOptionsProvider, namespace, name, output string, schema int, history, sensitive, force bool) error {
	opts, err := withReadScope(options(), namespace, false)
	if err != nil {
		return err
	}
	if opts.EvidenceMode == capability.Restricted {
		return fmt.Errorf("volume capture requires the authenticated collector volume profile")
	}
	reader, description, err := client.NewSnapshotReader(cmd.Context(), opts)
	if err != nil {
		return collectorUnavailableError(opts, description, err)
	}
	volumeReader, ok := reader.(incident.VolumeCaptureReader)
	if !ok {
		return fmt.Errorf("volume capture requires the authenticated Kubernetes API connection")
	}
	version := buildinfo.Current(runtime.Version(), runtime.GOOS, runtime.GOARCH).String()
	bundle, err := incident.CollectVolume(cmd.Context(), volumeReader, namespace, name, incident.VolumeCaptureOptions{IncludeHistory: history, IncludeSensitive: sensitive, ToolVersion: version})
	if err != nil {
		return err
	}
	if schema == 1 || schema == 2 {
		legacy, err := incident.LegacyVolume(bundle, schema)
		if err != nil {
			return err
		}
		err = incident.Write(cmd.OutOrStdout(), output, force, legacy)
		if err != nil {
			return err
		}
	} else {
		if err := incident.WriteVolume(cmd.OutOrStdout(), output, force, bundle); err != nil {
			return err
		}
	}
	if output != "-" {
		version := schema
		if version == 0 {
			version = incident.VolumeSchemaVersion
		}
		_, err = fmt.Fprintf(cmd.OutOrStdout(), "Wrote %s (incident schema %d; redacted=%t)\n", output, version, bundle.Redacted)
	}
	return err
}

func replayVolume(w io.Writer, b incident.VolumeBundle, podRef string) error {
	if podRef != "" && podRef != b.Pod.Namespace+"/"+b.Pod.PodName {
		return fmt.Errorf("selected Pod is not present in the volume incident")
	}
	var memory bytes.Buffer
	printPodExplanation(&memory, b.Pod)
	lines := []string{fmt.Sprintf("Volume incident captured: %s; redacted=%t", b.CapturedAt, b.Redacted)}
	lines = append(lines, nodeview.Wrap(strings.Split(memory.String(), "\n"), 100)...)
	result := explain.AnalyzeVolumes(explain.VolumeInput{Pod: b.Pod, Volumes: b.Volumes, Now: b.CapturedAt})
	lines = append(lines, volumeview.Lines(b.Volumes, result, b.CapturedAt, 100)...)
	for _, caveat := range b.Caveats {
		lines = append(lines, caveat)
	}
	if b.History != nil {
		memory.Reset()
		printPodHistory(&memory, []api.PodHistory{*b.History})
		lines = append(lines, nodeview.Wrap(strings.Split(memory.String(), "\n"), 100)...)
	}
	_, err := fmt.Fprintln(w, strings.Join(lines, "\n"))
	return err
}
