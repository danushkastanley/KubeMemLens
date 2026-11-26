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

func Write(stdout io.Writer, output string, overwrite bool, bundle api.IncidentBundle) error {
	bundle = api.WithoutIOIncident(bundle)
	if err := ValidateSchema(bundle); err != nil {
		return err
	}
	return writeDocument(stdout, output, overwrite, bundle)
}
