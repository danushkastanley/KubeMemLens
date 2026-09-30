package incident

import (
	"io"

	"github.com/danushkastanley/kube-memlens/internal/tracereport"
)

// WriteTrace publishes only an explicitly constructed redacted document.
// The caller must obtain export consent before invoking it.
func WriteTrace(stdout io.Writer, output string, overwrite bool, document tracereport.Document) error {
	data, err := document.Bytes()
	if err != nil {
		return err
	}
	return writeIncidentFile(stdout, output, overwrite, func(w io.Writer) error { _, err := w.Write(data); return err })
}
