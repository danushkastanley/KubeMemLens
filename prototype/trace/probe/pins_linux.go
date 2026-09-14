package probe

import (
	"errors"
	"golang.org/x/sys/unix"
	"io"
	"os"
)

// No pin is owned by this preflight profile. Anything in the reserved namespace
// is uncertain state for the administrator to investigate; never unlink it.
func reservedPinsClear(path string) (bool, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if errors.Is(err, os.ErrNotExist) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	dir := os.NewFile(uintptr(fd), path)
	defer dir.Close()
	names, err := dir.Readdirnames(1)
	if err != nil && !errors.Is(err, io.EOF) {
		return false, err
	}
	return len(names) == 0, nil
}
