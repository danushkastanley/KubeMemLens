package incident

import (
	"fmt"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/model"
)

func ValidateSchema(bundle api.IncidentBundle) error {
	if bundle.SchemaVersion != api.LegacySchemaVersion && bundle.SchemaVersion != api.CurrentIncidentSchemaVersion {
		return fmt.Errorf("unsupported incident schemaVersion %d; expected 1 or %d", bundle.SchemaVersion, api.CurrentIncidentSchemaVersion)
	}
	if bundle.SchemaVersion == api.LegacySchemaVersion && api.IncidentSchema(bundle.Pods) != api.LegacySchemaVersion {
		return fmt.Errorf("Pod resource and resize context requires incident schema 2")
	}
	for _, pod := range bundle.Pods {
		if err := (model.ContainerMemoryResources{Pod: pod.Context.Resources}).Validate(); err != nil {
			return fmt.Errorf("incident Pod resource context: %w", err)
		}
		for _, container := range pod.Containers {
			if err := container.Context.Resources.Validate(); err != nil {
				return fmt.Errorf("incident container resource context: %w", err)
			}
		}
	}
	return nil
}
