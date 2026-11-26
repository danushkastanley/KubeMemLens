package probe

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	p "github.com/danushkastanley/kube-memlens/internal/tracepreflight"
)

const maxDescriptors = 256

var errInventoryLimit = errors.New("descriptor inventory limit")

// object contains only the identity of an owned descriptor. Global BPF
// enumeration requires SYS_ADMIN and is deliberately outside this worker.
type object struct {
	kind string
	id   uint32
	tag  string
}

func snapshot(ctx context.Context, pid int) (map[int]object, error) {
	dir, err := os.Open(fmt.Sprintf("/proc/%d/fdinfo", pid))
	if err != nil {
		return nil, err
	}
	scanFD := int(dir.Fd())
	names, readErr := dir.Readdirnames(maxDescriptors + 1)
	closeErr := dir.Close()
	if err := errors.Join(readErr, closeErr); err != nil {
		return nil, err
	}
	if len(names) > maxDescriptors {
		return nil, errInventoryLimit
	}
	objects := map[int]object{}
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		fd, err := strconv.Atoi(name)
		if err != nil || fd < 0 || fd > 1<<20 {
			return nil, errors.New("invalid descriptor")
		}
		if pid == os.Getpid() && fd == scanFD {
			continue
		}
		data, err := readBounded(fmt.Sprintf("/proc/%d/fdinfo/%d", pid, fd), 4096)
		// Runtime descriptors can close between enumeration and inspection. This
		// process serialises BPF probes; no BPF descriptor is created concurrently.
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		obj, found, err := descriptorObject(data)
		if err != nil {
			return nil, err
		}
		if found {
			objects[fd] = obj
		}
	}
	return objects, nil
}

func descriptorObject(data []byte) (object, bool, error) {
	values := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		switch key {
		case "prog_id", "prog_tag", "map_id", "link_id":
			if _, duplicate := values[key]; duplicate {
				return object{}, false, errors.New("duplicate BPF identity")
			}
			values[key] = strings.TrimSpace(value)
		}
	}
	// Link fdinfo also describes its programme; the descriptor owns the link.
	for _, kind := range []string{"link", "map", "prog"} {
		value, found := values[kind+"_id"]
		if !found {
			continue
		}
		id, err := strconv.ParseUint(value, 10, 32)
		if err != nil || id == 0 {
			return object{}, false, errors.New("invalid BPF identity")
		}
		obj := object{kind: kind, id: uint32(id)}
		if kind == "prog" {
			obj.tag = values["prog_tag"]
			if !validTag(obj.tag) {
				return object{}, false, errors.New("invalid programme tag")
			}
		}
		return obj, true, nil
	}
	return object{}, false, nil
}

func validTag(tag string) bool { return len(tag) == 16 && strings.Trim(tag, "0123456789abcdef") == "" }

func (l *Local) ownership(ctx context.Context) p.Check {
	objects, err := snapshot(ctx, os.Getpid())
	if err != nil {
		return inventoryError(p.Ownership, err)
	}
	l.before = objects
	clear, err := reservedPinsClear("/sys/fs/bpf/kube-memlens-tracer")
	if err != nil || !clear {
		return result(p.Ownership, p.Degraded, p.OwnershipUncertain, "")
	}
	if state, reason := l.endedOwnership(ctx); state != p.Supported {
		return result(p.Ownership, state, reason, "")
	}
	if len(objects) != 0 {
		return result(p.Ownership, p.Degraded, p.OwnershipUncertain, "")
	}
	return result(p.Ownership, p.Supported, p.Available, "no-owned-descriptors")
}

func (l *Local) cleanup(ctx context.Context) p.Check {
	if l.before == nil {
		return result(p.ProbeCleanup, p.Degraded, p.PrerequisiteMissing, "")
	}
	after, err := snapshot(ctx, os.Getpid())
	if err != nil {
		return inventoryError(p.ProbeCleanup, err)
	}
	if len(after) > len(l.before) {
		return result(p.ProbeCleanup, p.Unsupported, p.OwnedOrphan, "")
	}
	if len(after) != len(l.before) {
		return result(p.ProbeCleanup, p.Degraded, p.OwnershipUncertain, "")
	}
	for fd, obj := range after {
		if previous, ok := l.before[fd]; !ok || previous != obj {
			return result(p.ProbeCleanup, p.Degraded, p.OwnershipUncertain, "")
		}
	}
	return result(p.ProbeCleanup, p.Supported, p.Available, "owned-descriptors-unchanged")
}

func inventoryError(id p.ID, err error) p.Check {
	if errors.Is(err, errInventoryLimit) {
		return result(id, p.Degraded, p.InventoryLimit, "")
	}
	if errors.Is(err, os.ErrPermission) {
		return result(id, p.Unsupported, p.InventoryDenied, "")
	}
	return result(id, p.Degraded, p.ProbeFailed, "")
}
