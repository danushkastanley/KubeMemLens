package verifier

import (
	"errors"
	"io"
	"os"

	"golang.org/x/sys/unix"
)

type traceFS struct {
	root     int
	commands map[string]bool
	paths    map[string]int64
}

func openTraceFS(plan ProbePlan) (*traceFS, error) {
	definitions, err := plan.Definitions()
	if err != nil {
		return nil, err
	}
	fd, err := unix.Open("/sys/kernel/tracing", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, ErrObservation
	}
	var stat unix.Statfs_t
	if unix.Fstatfs(fd, &stat) != nil || stat.Type != unix.TRACEFS_MAGIC {
		return nil, errors.Join(ErrObservation, unix.Close(fd))
	}
	fs := &traceFS{root: fd, commands: make(map[string]bool),
		paths: map[string]int64{"kprobe_events": 1 << 20, "kprobe_profile": 1 << 20}}
	for _, d := range definitions {
		fs.commands[d.Registration], fs.commands[d.Removal] = true, true
		fs.paths["events/"+d.Name+"/format"] = 16384
		fs.paths["events/"+d.Name+"/enable"] = 8
	}
	return fs, nil
}

func (f *traceFS) read(path string) ([]byte, error) {
	maximum, ok := f.paths[path]
	if !ok || f.root < 0 {
		return nil, ErrObservation
	}
	fd, err := unix.Openat(f.root, path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, ErrObservation
	}
	file := os.NewFile(uintptr(fd), "owned-tracefs-input")
	var stat unix.Statfs_t
	if unix.Fstatfs(fd, &stat) != nil || stat.Type != unix.TRACEFS_MAGIC {
		return nil, errors.Join(ErrObservation, file.Close())
	}
	raw, readErr := io.ReadAll(io.LimitReader(file, maximum+1))
	closeErr := file.Close()
	if readErr != nil || closeErr != nil || int64(len(raw)) > maximum {
		return nil, ErrObservation
	}
	return raw, nil
}

func (f *traceFS) registry() ([]byte, error) { return f.read("kprobe_events") }

func (f *traceFS) command(command string) error {
	if !f.commands[command] || f.root < 0 {
		return ErrObservation
	}
	// O_TRUNC would remove unrelated events. Only one fixed line is appended.
	fd, err := unix.Openat(f.root, "kprobe_events", unix.O_WRONLY|unix.O_APPEND|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return errors.Join(ErrObservation, err)
	}
	var stat unix.Statfs_t
	if unix.Fstatfs(fd, &stat) != nil || stat.Type != unix.TRACEFS_MAGIC {
		return errors.Join(ErrObservation, unix.Close(fd))
	}
	raw := []byte(command + "\n")
	n, writeErr := unix.Write(fd, raw)
	closeErr := unix.Close(fd)
	if writeErr != nil || closeErr != nil || n != len(raw) {
		return errors.Join(ErrObservation, writeErr, closeErr)
	}
	return nil
}

func (f *traceFS) close() error {
	if f.root < 0 {
		return nil
	}
	fd := f.root
	f.root = -1
	if unix.Close(fd) != nil {
		return ErrObservation
	}
	return nil
}
