package volumecontext

import (
	"fmt"
	"time"
)

const MaxWorkloadPods = 32

type VolumeMember struct {
	Pod    int `json:"pod"`
	Volume int `json:"volume"`
}

type FilesystemGroup struct {
	Members            []VolumeMember `json:"members"`
	Selected           VolumeMember   `json:"selected"`
	ObservationsDiffer bool           `json:"observationsDiffer"`
}

// GroupWorkload runs on already authorised server joins. It represents one
// shared PVC filesystem once, retaining every Pod mount and source observation.
// It never adds filesystem values or infers shared identity from names.
func GroupWorkload(pods []View, now time.Time) ([]FilesystemGroup, error) {
	if len(pods) > MaxWorkloadPods {
		return nil, ErrInvalid
	}
	groups := []FilesystemGroup{}
	byIdentity := map[string]int{}
	names := map[string]bool{}
	count := 0
	for pi, pod := range pods {
		if ValidateView(pod, now) != nil || (pi > 0 && pod.Namespace != pods[0].Namespace) || names[pod.PodName] {
			return nil, ErrInvalid
		}
		names[pod.PodName] = true
		for vi, volume := range pod.Volumes {
			count++
			if count > MaxPageRecords {
				return nil, ErrInvalid
			}
			key := volume.FilesystemID
			if key == "" {
				key = fmt.Sprintf("local-%d-%d", pi, vi)
			}
			member := VolumeMember{Pod: pi, Volume: vi}
			index, exists := byIdentity[key]
			if !exists {
				byIdentity[key] = len(groups)
				groups = append(groups, FilesystemGroup{Members: []VolumeMember{member}, Selected: member})
				continue
			}
			group := &groups[index]
			prior := pods[group.Selected.Pod].Volumes[group.Selected.Volume]
			group.Members = append(group.Members, member)
			currentFS, priorFS := workloadFilesystem(volume.Usage), workloadFilesystem(prior.Usage)
			if currentFS != nil && priorFS != nil && !sameFilesystemValues(*currentFS, *priorFS) {
				group.ObservationsDiffer = true
			}
			if currentFS != nil && (priorFS == nil || currentFS.CapturedAt.After(priorFS.CapturedAt)) {
				group.Selected = member
			}
		}
	}
	return groups, nil
}

func workloadFilesystem(usage Usage) *Filesystem {
	if usage.Filesystem != nil {
		return usage.Filesystem
	}
	return usage.LastGood
}
