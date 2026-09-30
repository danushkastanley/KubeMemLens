package api

import (
	"fmt"

	"github.com/danushkastanley/kube-memlens/internal/model"
)

func ValidateIncidentSchema(bundle IncidentBundle) error {
	if bundle.SchemaVersion != LegacySchemaVersion && bundle.SchemaVersion != CurrentIncidentSchemaVersion {
		return fmt.Errorf("unsupported incident schemaVersion %d; expected 1 or %d", bundle.SchemaVersion, CurrentIncidentSchemaVersion)
	}
	if bundle.SchemaVersion == LegacySchemaVersion && IncidentSchema(bundle.Pods) != LegacySchemaVersion {
		return fmt.Errorf("Pod resource and resize context requires incident schema 2")
	}
	for _, pod := range bundle.Pods {
		if pod.Memory.IOPressure.State != "" {
			return fmt.Errorf("cgroup I/O pressure requires a volume incident")
		}
		if err := (model.ContainerMemoryResources{Pod: pod.Context.Resources}).Validate(); err != nil {
			return fmt.Errorf("incident Pod resource context: %w", err)
		}
		for _, container := range pod.Containers {
			if container.Memory.IOPressure.State != "" {
				return fmt.Errorf("cgroup I/O pressure requires a volume incident")
			}
			if err := container.Context.Resources.Validate(); err != nil {
				return fmt.Errorf("incident container resource context: %w", err)
			}
		}
	}
	return nil
}
