package incident

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const MaxBytes int64 = 64 << 20

type boundedWriter struct {
	destination io.Writer
	remaining   int64
}

func (w *boundedWriter) Write(data []byte) (int, error) {
	if int64(len(data)) > w.remaining {
		return 0, fmt.Errorf("incident bundle exceeds %d byte limit", MaxBytes)
	}
	n, err := w.destination.Write(data)
	w.remaining -= int64(n)
	return n, err
}

// Stage even stdout output so an oversized document is never partly exported.
// Files are published with no-replace semantics unless overwrite was explicit.
func writeDocument(stdout io.Writer, output string, overwrite bool, document any) error {
	directory := filepath.Dir(output)
	if output == "-" {
		directory = ""
	}
	temporary, err := os.CreateTemp(directory, ".kube-memlens-incident-*")
	if err != nil {
		return fmt.Errorf("create incident file: %w", err)
	}
	name := temporary.Name()
	defer os.Remove(name)
	defer temporary.Close()
	if err := temporary.Chmod(0o600); err != nil {
		return fmt.Errorf("protect incident file: %w", err)
	}
	encoder := json.NewEncoder(&boundedWriter{destination: temporary, remaining: MaxBytes})
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(document); err != nil {
		return fmt.Errorf("encode incident file: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		return fmt.Errorf("sync incident file: %w", err)
	}
	if output == "-" {
		if _, err := temporary.Seek(0, io.SeekStart); err != nil {
			return err
		}
		_, err := io.Copy(stdout, temporary)
		return err
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close incident file: %w", err)
	}
	if overwrite {
		// Atomic replacement preserves the old destination if rename fails.
		if err := os.Rename(name, output); err != nil {
			return fmt.Errorf("replace incident file: %w", err)
		}
		return nil
	}
	// Linking in the same directory atomically fails if any destination exists,
	// including a symlink. Never fall back to an overwriting rename.
	if err := os.Link(name, output); err != nil {
		if os.IsExist(err) {
			return ExistsError{Path: output}
		}
		return fmt.Errorf("publish incident file: %w", err)
	}
	return nil
}
