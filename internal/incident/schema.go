package incident

import "github.com/danushkastanley/kube-memlens/internal/api"

func ValidateSchema(bundle api.IncidentBundle) error {
	return api.ValidateIncidentSchema(bundle)
}
