package probe

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/cilium/ebpf"
	"golang.org/x/sys/unix"
)

func TestDeadlineClosesHeldDiagnosticMap(t *testing.T) {
	if os.Getenv("KML_DESCRIPTOR_TEST") != "owned-local-test-worker" {
		t.Skip("requires disposable worker")
	}
	if os.Getenv("KML_HOLD_MAP") == "1" {
		ring, err := ebpf.NewMap(&ebpf.MapSpec{Name: "kml_pf_timeout", Type: ebpf.RingBuf, MaxEntries: uint32(os.Getpagesize())})
		if err != nil {
			os.Exit(91)
		}
		info, err := ring.Info()
		if err != nil {
			os.Exit(92)
		}
		id, ok := info.ID()
		if !ok {
			os.Exit(93)
		}
		fmt.Printf("HELD_MAP=%d\n", id)
		time.Sleep(time.Hour)
		ring.Close()
		os.Exit(94)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestDeadlineClosesHeldDiagnosticMap$")
	cmd.Env = append(os.Environ(), "KML_HOLD_MAP=1")
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: unix.SIGKILL}
	var output bytes.Buffer
	cmd.Stdout = &output
	start := time.Now()
	err := cmd.Run()
	if err == nil || ctx.Err() != context.DeadlineExceeded || cmd.ProcessState == nil {
		t.Fatal("worker was not killed and reaped at deadline")
	}
	if time.Since(start) > 3*time.Second {
		t.Fatal("worker exceeded shutdown budget")
	}
	id, err := strconv.ParseUint(strings.TrimSpace(strings.TrimPrefix(output.String(), "HELD_MAP=")), 10, 32)
	if err != nil || id == 0 {
		t.Fatal("worker did not establish a held diagnostic map")
	}
	if _, err := os.Stat(fmt.Sprintf("/proc/%d", cmd.Process.Pid)); !os.IsNotExist(err) {
		t.Fatal("terminated worker still exists")
	}
	// The administrator's post-run global census must exclude this ID.
	fmt.Printf("TIMED_OUT_MAP=%d\n", id)
}
