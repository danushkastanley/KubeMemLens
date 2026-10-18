package topologysource

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"strconv"
	"strings"
	"syscall"

	"github.com/danushkastanley/kube-memlens/internal/memorytopology"
)

func (b *readBudget) admit() error {
	if err := b.ctx.Err(); err != nil {
		return err
	}
	b.files++
	if b.files > MaxFiles {
		return memorytopology.ErrBounds
	}
	return nil
}
func (b *readBudget) read(root *os.Root, path string) ([]byte, error) {
	if err := b.admit(); err != nil {
		return nil, err
	}
	f, err := root.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, memorytopology.ErrInvalid
	}
	data, err := io.ReadAll(io.LimitReader(f, MaxFileBytes+1))
	if err != nil {
		return nil, err
	}
	b.bytes += len(data)
	if len(data) > MaxFileBytes || b.bytes > MaxReadBytes {
		return nil, memorytopology.ErrBounds
	}
	if err := b.ctx.Err(); err != nil {
		return nil, err
	}
	return data, nil
}
func (b *readBudget) entries(root *os.Root, path string) ([]os.DirEntry, error) {
	if err := b.admit(); err != nil {
		return nil, err
	}
	f, err := root.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	entries, err := f.ReadDir(MaxDirectoryEntries + 1)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	if len(entries) > MaxDirectoryEntries {
		return nil, memorytopology.ErrBounds
	}
	return entries, b.ctx.Err()
}
func (b *readBudget) scalar(root *os.Root, path string) (*uint64, error) {
	data, err := b.read(root, path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	value, err := number(strings.TrimSpace(string(data)))
	if err != nil {
		return nil, err
	}
	return &value, nil
}
func number(text string) (uint64, error) {
	if text == "" || len(text) > 20 {
		return 0, memorytopology.ErrInvalid
	}
	for _, c := range text {
		if c < '0' || c > '9' {
			return 0, memorytopology.ErrInvalid
		}
	}
	n, err := strconv.ParseUint(text, 10, 64)
	if err != nil {
		return 0, memorytopology.ErrInvalid
	}
	return n, nil
}
