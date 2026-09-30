package incident

import (
	"errors"
	"io"
	"os"

	"github.com/danushkastanley/kube-memlens/internal/incidentsession"
)

// WriteSession preserves the validated bounded encoding, including private-file
// permissions, staging and explicit overwrite semantics of other incident files.
func WriteSession(stdout io.Writer, path string, overwrite bool, data []byte) error {
	if _, err := incidentsession.DecodeExport(data); err != nil {
		return err
	}
	return writeIncidentFile(stdout, path, overwrite, func(w io.Writer) error {
		n, err := w.Write(data)
		if err == nil && n != len(data) {
			return io.ErrShortWrite
		}
		return err
	})
}

func ReadSession(path string) (incidentsession.DecodedExport, error) {
	file, err := os.Open(path)
	if err != nil {
		return incidentsession.DecodedExport{}, errors.New("cannot open incident session export")
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, incidentsession.MaxExportBytes+1))
	if err != nil {
		return incidentsession.DecodedExport{}, errors.New("cannot read incident session export")
	}
	return incidentsession.DecodeExport(data)
}
