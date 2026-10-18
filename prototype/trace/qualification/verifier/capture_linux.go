package verifier

import (
	"errors"
	"os"
	"runtime"
	"unsafe"

	"golang.org/x/sys/unix"
)

type Capture struct {
	fds                         []int
	rings                       []*perfRing
	counts                      []descriptorCount
	group                       *os.File
	inode                       uint64
	started, records, discarded uint64
	stopped, closed, failed     bool
	closeErr                    error
}

func monotonic() (uint64, error) {
	var t unix.Timespec
	if unix.ClockGettime(unix.CLOCK_MONOTONIC, &t) != nil || t.Sec < 0 || t.Nsec < 0 {
		return 0, ErrObservation
	}
	return uint64(t.Nano()), nil
}

func perfID(fd int) (uint64, error) {
	var id uint64
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), unix.PERF_EVENT_IOC_ID, uintptr(unsafe.Pointer(&id)))
	runtime.KeepAlive(&id)
	if errno != 0 || id == 0 {
		return 0, ErrObservation
	}
	return id, nil
}

// OpenCapture enables the three owned numeric probes only for the caller's
// verified cgroup and descendants. The file is duplicated and inode-pinned;
// the cgroup filesystem root is refused. Boot, process, topology and deadline
// checks belong to the enclosing owned-node controller.
func OpenCapture(probes *ProbeSet, group *os.File, inode uint64, cpus []uint32) (capture *Capture, err error) {
	if probes == nil || group == nil || inode == 0 {
		return nil, ErrObservation
	}
	if _, err := NewMerge(cpus); err != nil {
		return nil, err
	}
	formats, _, err := probes.Formats()
	if err != nil {
		return nil, err
	}
	fd, err := unix.FcntlInt(group.Fd(), unix.F_DUPFD_CLOEXEC, 0)
	if err != nil {
		return nil, ErrObservation
	}
	c := &Capture{group: os.NewFile(uintptr(fd), "owned-verifier-cgroup"), inode: inode}
	defer func() {
		if err != nil {
			err = errors.Join(err, c.Close())
		}
	}()
	if err = c.groupValid(); err != nil {
		return nil, err
	}
	var root unix.Stat_t
	if unix.Stat("/sys/fs/cgroup", &root) != nil || root.Ino == inode {
		return nil, ErrObservation
	}
	for _, cpu := range cpus {
		if err = c.openCPU(cpu, formats); err != nil {
			return nil, err
		}
	}
	c.counts = make([]descriptorCount, len(c.fds))
	for _, fd := range c.fds {
		if unix.IoctlSetInt(fd, unix.PERF_EVENT_IOC_ENABLE, 0) != nil {
			return nil, ErrObservation
		}
	}
	c.started, err = monotonic()
	if err != nil {
		return nil, err
	}
	return c, nil
}

func (c *Capture) groupValid() error {
	if c.group == nil {
		return ErrObservation
	}
	var stat unix.Stat_t
	var fs unix.Statfs_t
	fd := int(c.group.Fd())
	if unix.Fstat(fd, &stat) != nil || unix.Fstatfs(fd, &fs) != nil || fs.Type != unix.CGROUP2_SUPER_MAGIC ||
		stat.Ino != c.inode || stat.Nlink == 0 || stat.Mode&unix.S_IFMT != unix.S_IFDIR {
		return ErrObservation
	}
	return nil
}

func (c *Capture) openCPU(cpu uint32, formats []Format) error {
	pageSize := os.Getpagesize()
	if pageSize < 4096 || pageSize > 65536 || pageSize&(pageSize-1) != 0 {
		return ErrObservation
	}
	output := -1
	byID := make(map[uint64]Format)
	for _, format := range formats {
		attr := unix.PerfEventAttr{Type: unix.PERF_TYPE_TRACEPOINT, Config: uint64(format.id), Sample: 1,
			Read_format: unix.PERF_FORMAT_TOTAL_TIME_ENABLED | unix.PERF_FORMAT_TOTAL_TIME_RUNNING | unix.PERF_FORMAT_LOST,
			Sample_type: unix.PERF_SAMPLE_IDENTIFIER | unix.PERF_SAMPLE_TID | unix.PERF_SAMPLE_TIME | unix.PERF_SAMPLE_CPU | unix.PERF_SAMPLE_RAW,
			Bits:        unix.PerfBitDisabled | unix.PerfBitSampleIDAll | unix.PerfBitUseClockID, Clockid: unix.CLOCK_MONOTONIC, Wakeup: 1}
		attr.Size = uint32(unsafe.Sizeof(attr))
		fd, err := unix.PerfEventOpen(&attr, int(c.group.Fd()), int(cpu), -1, unix.PERF_FLAG_FD_CLOEXEC|unix.PERF_FLAG_PID_CGROUP)
		if err != nil {
			return errors.Join(ErrObservation, err)
		}
		c.fds = append(c.fds, fd)
		id, err := perfID(fd)
		if err != nil {
			return err
		}
		if _, exists := byID[id]; exists {
			return ErrObservation
		}
		byID[id] = format
		if output != -1 {
			if unix.IoctlSetInt(fd, unix.PERF_EVENT_IOC_SET_OUTPUT, output) != nil {
				return ErrObservation
			}
			continue
		}
		output = fd
		mapping, err := unix.Mmap(fd, 0, pageSize+ringBytes, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
		if err != nil {
			return ErrObservation
		}
		ring, err := bindRing(mapping, pageSize, cpu, byID)
		if err != nil {
			return errors.Join(err, unix.Munmap(mapping))
		}
		c.rings = append(c.rings, ring)
	}
	return nil
}

func (c *Capture) Started() uint64          { return c.started }
func (c *Capture) StartupDiscarded() uint64 { return c.discarded }

func (c *Capture) Drain(consume func(Frame) error) error {
	if c.closed || c.failed || consume == nil || c.groupValid() != nil {
		c.failed = true
		return ErrObservation
	}
	for _, ring := range c.rings {
		discarded, err := ring.drain(c.started, func(frame Frame) error {
			if c.records >= maxEvents {
				return ErrObservation
			}
			c.records++
			return consume(frame)
		})
		c.discarded += discarded
		if err != nil || c.discarded > maxEvents {
			c.failed = true
			return ErrObservation
		}
	}
	return nil
}

// Stop disables descriptors before draining their last records and accounting
// for every event. It must succeed before a complete observation is reported.
func (c *Capture) Stop(consume func(Frame) error) (PerfCounters, error) {
	if c.closed || c.stopped || c.failed {
		return PerfCounters{}, ErrObservation
	}
	if c.disable() != nil {
		c.failed = true
		return PerfCounters{}, ErrObservation
	}
	c.stopped = true
	if err := c.Drain(consume); err != nil {
		return PerfCounters{}, err
	}
	result, err := c.Counters()
	if err != nil || result.Events != c.records+c.discarded {
		c.failed = true
		return PerfCounters{}, ErrObservation
	}
	return result, nil
}

func (c *Capture) disable() error {
	var result error
	for _, fd := range c.fds {
		if unix.IoctlSetInt(fd, unix.PERF_EVENT_IOC_DISABLE, 0) != nil {
			result = errors.Join(result, ErrObservation)
		}
	}
	return result
}

func (c *Capture) Close() error {
	if c.closed {
		return c.closeErr
	}
	c.closed = true
	c.closeErr = c.disable()
	for _, ring := range c.rings {
		c.closeErr = errors.Join(c.closeErr, unix.Munmap(ring.mapping))
	}
	for _, fd := range c.fds {
		c.closeErr = errors.Join(c.closeErr, unix.Close(fd))
	}
	if c.group != nil {
		c.closeErr = errors.Join(c.closeErr, c.group.Close())
	}
	c.fds, c.rings = nil, nil
	c.group = nil
	return c.closeErr
}
