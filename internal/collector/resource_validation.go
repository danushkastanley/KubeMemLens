package collector

import (
	"fmt"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/model"
)

type podResourceKey struct {
	namespace string
	name      string
	uid       string
}

func validatePodResourceAgreement(containers []api.ContainerSnapshot) error {
	var observed map[podResourceKey]model.PodMemoryResources
	for _, container := range containers {
		resources := container.Context.Resources.Pod
		if resources.IsZero() {
			continue
		}
		if observed == nil {
			observed = make(map[podResourceKey]model.PodMemoryResources)
		}
		key := podResourceKey{container.Namespace, container.PodName, container.PodUID}
		if previous, found := observed[key]; found && previous != resources {
			return fmt.Errorf("Pod resource context differs between containers in the same snapshot")
		}
		observed[key] = resources
	}
	return nil
}
