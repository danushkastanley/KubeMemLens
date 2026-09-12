package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"strings"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/volumecontext"
	"k8s.io/apimachinery/pkg/util/validation"
)

func (v *WorkloadVolumeContext) UnmarshalJSON(data []byte) error {
	if len(data) > volumecontext.MaxPageBytes {
		return fmt.Errorf("workload volume response exceeds byte limit")
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	if err := workloadVolumeValue(d, "", 0, map[string]int{}); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return fmt.Errorf("invalid workload volume JSON")
	}
	type wire WorkloadVolumeContext
	var result wire
	d = json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(&result); err != nil {
		return fmt.Errorf("invalid workload volume fields")
	}
	*v = WorkloadVolumeContext(result)
	return nil
}

func workloadVolumeValue(d *json.Decoder, field string, depth int, counts map[string]int) error {
	invalid := fmt.Errorf("invalid bounded workload volume response")
	counts["tokens"]++
	if depth > 24 || counts["tokens"] > 65536 {
		return invalid
	}
	token, err := d.Token()
	if err != nil {
		return invalid
	}
	delim, compound := token.(json.Delim)
	if !compound {
		if value, ok := token.(string); ok && len(value) > 4096 {
			return invalid
		}
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]bool{}
		for d.More() {
			token, err := d.Token()
			key, ok := token.(string)
			if err != nil || !ok || len(key) > 64 || len(seen) >= 256 || seen[strings.ToLower(key)] {
				return invalid
			}
			seen[strings.ToLower(key)] = true
			if err := workloadVolumeValue(d, key, depth+1, counts); err != nil {
				return err
			}
		}
	case '[':
		limit := 0
		switch field {
		case "pods", "podVolumes", "unscheduledPods":
			limit = volumecontext.MaxWorkloadPods
		case "volumes":
			limit = volumecontext.MaxVolumesPerPod
		case "containers":
			limit = 256
		case "health":
			limit = 3
		case "conditions":
			limit = 16
		case "filesystems", "members":
			limit = volumecontext.MaxPageRecords
		}
		for count := 0; d.More(); count++ {
			if count >= limit {
				return invalid
			}
			counts[field]++
			if (field == "containers" && counts[field] > 1024) || ((field == "volumes" || field == "members") && counts[field] > volumecontext.MaxPageRecords) {
				return invalid
			}
			if err := workloadVolumeValue(d, "", depth+1, counts); err != nil {
				return err
			}
		}
	default:
		return invalid
	}
	_, err = d.Token()
	return err
}

func ValidateWorkloadVolumeContext(v WorkloadVolumeContext, now time.Time) error {
	invalid := fmt.Errorf("workload volume response does not match its bounded scope")
	if v.APIVersion != MemoryAPIGroup+"/"+MemoryAPIVersion || v.Kind != "WorkloadVolumeContext" || v.UID == "" || len(v.UID) > volumecontext.MaxUIDBytes || len(validation.IsDNS1123Label(v.Namespace)) != 0 || len(validation.IsDNS1123Subdomain(v.Name)) != 0 || v.ObservedAt.IsZero() || v.ObservedAt.After(now.Add(volumecontext.FutureSkew)) {
		return invalid
	}
	w := v.Workload
	if w.Namespace != v.Namespace || w.Name != v.Name || w.PodCount != len(w.Pods) || len(w.Pods) > volumecontext.MaxWorkloadPods || len(v.PodVolumes)+len(v.UnscheduledPods) > volumecontext.MaxWorkloadPods {
		return invalid
	}
	switch w.Kind {
	case "Deployment", "ReplicaSet", "StatefulSet", "DaemonSet", "ReplicationController", "Job", "CronJob":
	default:
		return invalid
	}
	views := make([]volumecontext.View, 0, len(v.PodVolumes))
	instances := map[string]string{}
	for _, pod := range v.PodVolumes {
		if pod.APIVersion != MemoryAPIGroup+"/"+MemoryAPIVersion || pod.Kind != "PodVolumeContext" || pod.Namespace != v.Namespace || pod.Context.Namespace != v.Namespace || pod.Name != pod.Context.PodName || pod.UID == "" || len(pod.UID) > volumecontext.MaxUIDBytes || instances[string(pod.UID)] != "" {
			return invalid
		}
		instances[string(pod.UID)] = pod.Name
		views = append(views, pod.Context)
	}
	seen := map[string]bool{}
	for _, pod := range v.UnscheduledPods {
		if pod.Namespace != v.Namespace || pod.UID == "" || len(pod.UID) > volumecontext.MaxUIDBytes || instances[string(pod.UID)] != "" || seen[string(pod.UID)] || len(validation.IsDNS1123Subdomain(pod.Name)) != 0 || pod.Reason != "not-scheduled" {
			return invalid
		}
		seen[string(pod.UID)] = true
	}
	containers := 0
	for _, pod := range w.Pods {
		if pod.Namespace != v.Namespace || pod.PodUID == "" || instances[pod.PodUID] != pod.PodName || seen[pod.PodUID] {
			return invalid
		}
		seen[pod.PodUID] = true
		for _, container := range pod.Containers {
			containers++
			if containers > 1024 || container.PodUID != pod.PodUID || container.Namespace != pod.Namespace || container.PodName != pod.PodName || container.Memory.IOPressure.Validate() != nil {
				return invalid
			}
		}
	}
	groups, err := volumecontext.GroupWorkload(views, now)
	if err != nil || !reflect.DeepEqual(groups, v.Filesystems) {
		return invalid
	}
	return nil
}
