package incident

import (
	"fmt"
	"io"

	"github.com/danushkastanley/kube-memlens/internal/api"
)

type ExistsError struct {
	Path string
}

func (err ExistsError) Error() string {
	return fmt.Sprintf("output file %s already exists; explicit overwrite confirmation is required", err.Path)
}

func Redact(bundle *api.IncidentBundle) {
	for index := range bundle.Pods {
		bundle.Pods[index].PodUID = ""
		bundle.Pods[index].Context.Labels = nil
		for containerIndex := range bundle.Pods[index].Containers {
			container := &bundle.Pods[index].Containers[containerIndex]
			container.PodUID = ""
			container.ContainerID = ""
			container.CgroupPath = ""
			container.Context.Labels = nil
		}
	}
	for index := range bundle.Histories {
		bundle.Histories[index].PodUID = ""
	}
}

func Write(stdout io.Writer, output string, overwrite bool, bundle api.IncidentBundle) error {
	bundle = api.WithoutIOIncident(bundle)
	if err := ValidateSchema(bundle); err != nil {
		return err
	}
	return writeDocument(stdout, output, overwrite, bundle)
}
