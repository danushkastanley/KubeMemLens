package scheduler

import (
	"errors"
	"os"
	"runtime"
	"unsafe"

	"golang.org/x/sys/unix"
)

type Capture struct {
	fds       []int
	rings     []*perfRing
	started   uint64
	discarded uint64
	records   uint64
	closed    bool
	failed    bool
	closeErr  error
}

func monotonic() (uint64, error) {
	var value unix.Timespec
	if unix.ClockGettime(unix.CLOCK_MONOTONIC, &value) != nil || value.Sec < 0 || value.Nsec < 0 {
		return 0, ErrObservation
	}
	return uint64(value.Nano()), nil
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

// OpenCapture opens only the four validated scheduling tracepoints. It never
// sets tracefs enable flags, creates probes, loads BPF, or alters kernel settings.
// The caller must bind boot/topology/format hashes and a hard process lifetime.
func OpenCapture(cpus []uint32, formats []Format) (capture *Capture, err error) {
	if runtime.GOARCH != "arm64" && runtime.GOARCH != "amd64" {
		return nil, ErrObservation
	}
	if _, err = NewMerge(cpus, 1); err != nil || len(formats) != 4 {
		return nil, ErrObservation
	}
	kinds := map[Kind]bool{}
	ids := map[uint16]bool{}
	for _, f := range formats {
		if f.kind < Wake || f.kind > Exit || f.id == 0 || kinds[f.kind] || ids[f.id] {
			return nil, ErrObservation
		}
		kinds[f.kind] = true
		ids[f.id] = true
	}
	c := &Capture{}
	defer func() {
		if err != nil {
			err = errors.Join(err, c.Close())
		}
	}()
	for _, cpu := range cpus {
		if err = c.openCPU(cpu, formats); err != nil {
			return nil, err
		}
	}
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
			Sample_type: unix.PERF_SAMPLE_IDENTIFIER | unix.PERF_SAMPLE_TIME | unix.PERF_SAMPLE_CPU | unix.PERF_SAMPLE_RAW,
			Bits:        unix.PerfBitDisabled | unix.PerfBitSampleIDAll | unix.PerfBitUseClockID,
			Clockid:     unix.CLOCK_MONOTONIC, Wakeup: 1}
		attr.Size = uint32(unsafe.Sizeof(attr))
		fd, err := unix.PerfEventOpen(&attr, -1, int(cpu), -1, unix.PERF_FLAG_FD_CLOEXEC)
		if err != nil {
			return ErrObservation
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
	if c.closed || c.failed || consume == nil {
		return ErrObservation
	}
	for _, ring := range c.rings {
		discarded, err := ring.drain(c.started, func(frame Frame) error {
			if c.records >= 100000000 {
				return ErrObservation
			}
			c.records++
			return consume(frame)
		})
		c.discarded += discarded
		if err != nil {
			c.failed = true
			return err
		}
	}
	return nil
}

// Close disables and closes every owned descriptor even after an earlier error.
// FDs are CLOEXEC and never inherited; closing them removes these perf sessions.
func (c *Capture) Close() error {
	if c.closed {
		return c.closeErr
	}
	c.closed = true
	var result error
	for _, fd := range c.fds {
		if unix.IoctlSetInt(fd, unix.PERF_EVENT_IOC_DISABLE, 0) != nil {
			result = ErrObservation
		}
	}
	// Check every final counter after disabling events, so a pending LOST
	// record or paused event cannot be hidden by the last emitted sample.
	if result == nil && len(c.fds) > 0 {
		_, result = readCounters(c.fds, unix.Read)
	}
	for _, ring := range c.rings {
		if unix.Munmap(ring.mapping) != nil {
			result = ErrObservation
		}
		ring.mapping = nil
		ring.data = nil
		ring.page = nil
	}
	for i := len(c.fds) - 1; i >= 0; i-- {
		if unix.Close(c.fds[i]) != nil {
			result = ErrObservation
		}
	}
	c.fds = nil
	c.rings = nil
	c.closeErr = result
	return result
}
