package traceclient

import (
	"context"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"strings"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/trace"
	admission "github.com/danushkastanley/kube-memlens/internal/traceadmission"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/validation"
)

func targetNames(namespace, pod, container string) bool {
	return len(validation.IsDNS1123Label(namespace)) == 0 && len(validation.IsDNS1123Subdomain(pod)) == 0 && len(validation.IsDNS1123Label(container)) == 0
}

// Select reads the actual container lifetime with the operator's credentials.
// Callers with an already displayed selection must use SelectPinned instead.
func (c *Client) Select(ctx context.Context, namespace, pod, container string) (Selection, error) {
	if !targetNames(namespace, pod, container) {
		return Selection{}, failure(Invalid)
	}
	data, _, err := c.control(ctx, "GET", "/api/v1/namespaces/"+namespace+"/pods/"+pod, nil, 200, 4<<20)
	if err != nil {
		return Selection{}, err
	}
	var current corev1.Pod
	if jsonv2.Unmarshal(data, &current) != nil || current.Namespace != namespace || current.Name != pod || current.DeletionTimestamp != nil || current.UID == "" || len(current.Status.ContainerStatuses) > 128 {
		return Selection{}, failure(TargetChanged)
	}
	var selected *corev1.ContainerStatus
	for index := range current.Status.ContainerStatuses {
		status := &current.Status.ContainerStatuses[index]
		if status.Name != container {
			continue
		}
		if selected != nil {
			return Selection{}, failure(Protocol)
		}
		selected = status
	}
	if selected == nil || selected.State.Running == nil || selected.RestartCount < 0 || !strings.HasPrefix(selected.ContainerID, "containerd://") {
		return Selection{}, failure(TargetChanged)
	}
	result := Selection{Namespace: namespace, Pod: pod, PodUID: string(current.UID), Container: container, ContainerID: strings.TrimPrefix(selected.ContainerID, "containerd://"), ContainerStartedAt: selected.State.Running.StartedAt.Time.UTC(), NodeName: current.Spec.NodeName}
	if _, err := requestData(result, DefaultIntent(trace.Files)); err != nil {
		return Selection{}, failure(TargetChanged)
	}
	return result, nil
}

func (c *Client) SelectPinned(ctx context.Context, namespace, pod, container string, pin SelectionPin) (Selection, error) {
	id := strings.TrimPrefix(pin.ContainerID, "containerd://")
	if pin.PodUID == "" || len(pin.PodUID) > 128 || len(id) != 64 || strings.Trim(id, "0123456789abcdef") != "" || len(validation.IsDNS1123Subdomain(pin.NodeName)) != 0 {
		return Selection{}, failure(Invalid)
	}
	selected, err := c.Select(ctx, namespace, pod, container)
	if err != nil {
		return Selection{}, err
	}
	if selected.PodUID != pin.PodUID || selected.ContainerID != id || selected.NodeName != pin.NodeName {
		return Selection{}, failure(TargetChanged)
	}
	return selected, nil
}

func requestData(s Selection, intent Intent) ([]byte, error) {
	if !targetNames(s.Namespace, s.Pod, s.Container) || trace.ValidateIntent(intent.Kind, intent.Paths, intent.Bounds) != nil || intent.Bounds.Duration%time.Second != 0 {
		return nil, failure(Invalid)
	}
	body := map[string]any{"schemaVersion": 2, "pod": s.Pod, "container": s.Container, "kind": intent.Kind, "rawPaths": intent.Paths == trace.ConfirmedPaths,
		"durationSeconds": uint64(intent.Bounds.Duration / time.Second), "maxEvents": intent.Bounds.Events, "maxOutputBytes": intent.Bounds.OutputBytes, "maxMapBytes": intent.Bounds.MapBytes, "maxPathBytes": intent.Bounds.PathBytes,
		"expectedPodUID": s.PodUID, "expectedContainerID": s.ContainerID, "expectedContainerStartedAt": s.ContainerStartedAt.UTC(), "expectedNodeName": s.NodeName}
	data, err := json.Marshal(body)
	if err != nil {
		return nil, failure(Invalid)
	}
	if _, err := admission.DecodeRequest(s.Namespace, strings.NewReader(string(data))); err != nil {
		return nil, failure(Invalid)
	}
	return data, nil
}
