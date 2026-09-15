//go:build linux || darwin

package workerinstall

import (
	"io"
	"os"
	"path/filepath"
	"syscall"
)

// ReadFile reads installation policy once from a trusted, read-only deployment
// location. Root permits Kubernetes projected-file symlinks within that directory
// while rejecting escapes. Parsed bytes are later transferred in a sealed memfd.
func ReadFile(path string) (*Policy, error) {
	if !filepath.IsAbs(path) {
		return nil, ErrInstallation
	}
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return nil, ErrInstallation
	}
	defer root.Close()
	file, err := root.OpenFile(filepath.Base(path), os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, ErrInstallation
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0022 != 0 || info.Size() <= 0 || info.Size() > MaxPolicyBytes {
		return nil, ErrInstallation
	}
	data, err := io.ReadAll(io.LimitReader(file, MaxPolicyBytes+1))
	if err != nil {
		return nil, ErrInstallation
	}
	return Parse(data)
}
