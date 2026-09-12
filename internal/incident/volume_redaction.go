package incident

import (
	"fmt"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/volumecontext"
	"github.com/danushkastanley/kube-memlens/internal/volumehealth"
)

func redactVolume(b *VolumeBundle) {
	b.Redacted = true
	p := &b.Pod
	p.Namespace, p.PodName = "namespace-1", "pod-1"
	p.PodUID, p.NodeName, p.Memory.Name = "", "", ""
	p.Context = redactedVolumePodContext(p.Context)
	for i := range p.Containers {
		c := &p.Containers[i]
		c.Namespace, c.PodName = p.Namespace, p.PodName
		c.PodUID, c.NodeName, c.ContainerID, c.CgroupPath, c.Memory.Name = "", "", "", "", ""
		c.ContainerName = fmt.Sprintf("container-%d", i+1)
		c.Context = redactedVolumeContainerContext(c.Context)
	}
	b.Volumes.Namespace, b.Volumes.PodName = p.Namespace, p.PodName
	for i := range b.Volumes.Volumes {
		v := &b.Volumes.Volumes[i]
		v.VolumeName = fmt.Sprintf("volume-%d", i+1)
		v.PVCName, v.Driver, v.EvidenceID, v.FilesystemID = "", "", "", ""
		for j := range v.Health {
			redactVolumeHealth(&v.Health[j].HealthReport)
			if v.Health[j].LastGood != nil {
				redactVolumeHealth(v.Health[j].LastGood)
			}
		}
	}
	if b.History != nil {
		b.History.Namespace, b.History.PodName = p.Namespace, p.PodName
		b.History.PodUID, b.History.NodeName = "", ""
	}
}

func redactedVolumePodContext(c api.PodContext) api.PodContext {
	c.Labels = nil
	c.OwnerKind, c.OwnerName, c.WorkloadKind, c.WorkloadName, c.RuntimeClassName = "", "", "", "", ""
	c.Phase = volumeVocabulary(c.Phase, "Pending", "Running", "Succeeded", "Failed", "Unknown")
	c.QoSClass = volumeVocabulary(c.QoSClass, "Guaranteed", "Burstable", "BestEffort")
	c.NodeMemoryPressure = volumeVocabulary(c.NodeMemoryPressure, "True", "False", "Unknown")
	c.LastTerminationReason = volumeVocabulary(c.LastTerminationReason, "OOMKilled", "Completed", "Error", "Unknown")
	return c
}
func redactedVolumeContainerContext(c api.ContainerContext) api.ContainerContext {
	c.Labels = nil
	c.OwnerKind, c.OwnerName, c.WorkloadKind, c.WorkloadName, c.RuntimeClassName = "", "", "", "", ""
	c.PodPhase = volumeVocabulary(c.PodPhase, "Pending", "Running", "Succeeded", "Failed", "Unknown")
	c.QoSClass = volumeVocabulary(c.QoSClass, "Guaranteed", "Burstable", "BestEffort")
	c.NodeMemoryPressure = volumeVocabulary(c.NodeMemoryPressure, "True", "False", "Unknown")
	c.LastTerminationReason = volumeVocabulary(c.LastTerminationReason, "OOMKilled", "Completed", "Error", "Unknown")
	return c
}
func volumeVocabulary(value string, allowed ...string) string {
	if value == "" {
		return ""
	}
	for _, item := range allowed {
		if value == item {
			return value
		}
	}
	return "Unknown"
}
func redactVolumeHealth(h *volumecontext.HealthReport) {
	h.Observation.Identity = volumehealth.Identity{}
	h.Observation.Conditions = nil
	for i := range h.Conditions {
		c := &h.Conditions[i]
		c.Reason, c.AccessMode, c.VolumeMode = "", "", ""
		known := false
		switch h.Observation.Source {
		case volumehealth.BackendSource:
			known = c.Status == "StorageUnreachable" || c.Status == "StorageDegraded"
		default:
			known = c.Status == "Inaccessible" || c.Status == "DataLoss" || c.Status == "Degraded"
		}
		if !known {
			c.Status = "UnknownCondition"
		}
	}
}
