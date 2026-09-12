package client

import (
	"context"
	"fmt"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/volumecontext"
)

type PodVolumeReader interface {
	PodReader
	VolumeContextReader
}

// ReadPodVolumeEvidence binds memory to a current authorised volume query.
// Any failure returns neither domain, including revocation after memory read.
func ReadPodVolumeEvidence(ctx context.Context, reader PodVolumeReader, namespace, name, expectedUID string) (api.PodSnapshot, api.PodVolumeContext, error) {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	pod, err := reader.Pod(ctx, namespace, name)
	if err != nil {
		return api.PodSnapshot{}, api.PodVolumeContext{}, err
	}
	if pod.Namespace != namespace || pod.PodName != name || pod.PodUID == "" || (expectedUID != "" && pod.PodUID != expectedUID) {
		return api.PodSnapshot{}, api.PodVolumeContext{}, readDecodeError("get volume memory evidence", fmt.Errorf("memory response does not match the selected Pod instance"))
	}
	volumes, err := reader.PodVolumes(ctx, namespace, name, pod.PodUID)
	if err != nil {
		return api.PodSnapshot{}, api.PodVolumeContext{}, err
	}
	if string(volumes.UID) != pod.PodUID || volumes.Namespace != namespace || volumes.Name != name || volumes.Context.Namespace != namespace || volumes.Context.PodName != name || volumecontext.ValidateView(volumes.Context, time.Now().UTC()) != nil {
		return api.PodSnapshot{}, api.PodVolumeContext{}, readDecodeError("get volume memory evidence", fmt.Errorf("volume response does not match the selected Pod instance"))
	}
	if ctx.Err() != nil {
		return api.PodSnapshot{}, api.PodVolumeContext{}, ctx.Err()
	}
	return pod, volumes, nil
}
