package main

import (
	"io"
	"os"
	"strings"
	"syscall"

	"github.com/danushkastanley/kube-memlens/internal/traceaudit"
)

// Kubernetes projected Secret symlinks are allowed. Open non-blocking, then
// inspect the opened descriptor so a pipe or device cannot stall key loading.
func loadAuditReferences(path, expected string) (*traceaudit.References, error) {
	if len(expected) != 64 || strings.Trim(expected, "0123456789abcdef") != "" {
		return nil, traceaudit.ErrInvalid
	}
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, traceaudit.ErrInvalid
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() != 32 {
		return nil, traceaudit.ErrInvalid
	}
	key, err := io.ReadAll(io.LimitReader(file, 33))
	if err != nil {
		return nil, traceaudit.ErrInvalid
	}
	defer clear(key)
	refs, err := traceaudit.NewReferences(key)
	if err != nil || refs.KeyID() != "sha256:"+expected {
		return nil, traceaudit.ErrInvalid
	}
	return refs, nil
}
