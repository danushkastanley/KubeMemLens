package tui

import (
	"fmt"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/client"
	"github.com/danushkastanley/kube-memlens/internal/explain"
	"github.com/danushkastanley/kube-memlens/internal/nodeview"
	"github.com/danushkastanley/kube-memlens/internal/volumeview"
)

func (m appModel) workloadVolumeDetailLines(width int) []string {
	s := m.selectedVolumes
	lines := []string{"Workload volume context | e memory | h back | y copy command", m.volumeRefreshLabel()}
	if _, ok := m.client.(client.WorkloadVolumeReader); !ok {
		return nodeview.Wrap(append(lines, "The authenticated workload volume profile is required."), width)
	}
	if s.inFlight {
		lines = append(lines, "Refreshing authorised workload membership and volume evidence...")
	}
	if s.err != nil {
		message := "Workload volume source is unavailable; retained samples keep their original times."
		if client.IsForbidden(s.err) {
			message = "Workload volume access denied; protected evidence and actions cleared."
		}
		if client.IsNotFound(s.err) {
			message = "The workload or optional workload volume profile was not found."
		}
		lines = append(lines, message)
	}
	if s.workload == nil {
		return nodeview.Wrap(append(lines, "No authorised workload volume evidence available."), width)
	}
	now := time.Now().UTC()
	result := explain.AnalyzeWorkloadVolumes(*s.workload, now)
	return append(nodeview.Wrap(lines, width), volumeview.WorkloadLines(*s.workload, result, now, width)...)
}

func (m appModel) workloadVolumeSummaryLines(namespace, kind, name string) []string {
	s := m.selectedVolumes
	if s.namespace != namespace || s.kind != kind || s.name != name {
		return nil
	}
	if client.IsForbidden(s.err) {
		return []string{"Workload volume context: access denied."}
	}
	if s.workload == nil {
		return nil
	}
	return []string{fmt.Sprintf("Volume context: %d live Pods, %d filesystem groups; v opens details", len(s.workload.PodVolumes), len(s.workload.Filesystems))}
}
