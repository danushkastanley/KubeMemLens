package workerruntime

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

type resources struct {
	target *os.File
	image  *os.File
	bundle *os.File
	policy *os.File
	root   *os.Root
}

func (r *resources) close() error {
	var failures []error
	for _, file := range []*os.File{r.target, r.image, r.bundle, r.policy} {
		if file != nil {
			failures = append(failures, file.Close())
		}
	}
	if r.root != nil {
		failures = append(failures, r.root.Close())
	}
	return errors.Join(failures...)
}

func duplicate(file *os.File) (*os.File, error) {
	fd, err := unix.FcntlInt(file.Fd(), unix.F_DUPFD_CLOEXEC, 0)
	if err != nil {
		return nil, ErrRuntime
	}
	return os.NewFile(uintptr(fd), "private-worker-resource"), nil
}

// snapshot is called with the runtime lock held. Each execution owns separate
// descriptor numbers; sealed image/policy contents and the directory are shared.
func (r *Runtime) snapshot() (*resources, error) {
	if r.closed || r.poisoned {
		return nil, ErrRuntime
	}
	out := &resources{}
	var err error
	for _, pair := range []struct {
		source      *os.File
		destination **os.File
	}{
		{r.owned.image, &out.image}, {r.owned.bundle, &out.bundle}, {r.owned.policy, &out.policy},
	} {
		*pair.destination, err = duplicate(pair.source)
		if err != nil {
			if out.close() != nil {
				r.poisoned = true
				r.cancel()
			}
			return nil, ErrRuntime
		}
	}
	return out, nil
}
