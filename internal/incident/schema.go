package incident

import "github.com/danushkastanley/kube-memlens/internal/api"

func ValidateSchema(bundle api.IncidentBundle) error {
	if err := api.ValidateIncidentSchema(bundle); err != nil {
		return err
	}
	return validateDeepText(bundle)
}
