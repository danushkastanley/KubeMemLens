package probe

import (
	"errors"
	"os"
	"testing"

	"github.com/cilium/ebpf"
	"golang.org/x/sys/unix"
)

// This explicit negative integration test uses only the existing, fixed,
// non-attaching preflight probes. It refuses to run with any available capability.
func TestBoundedProbeLoadsAreDeniedInCaplessSandbox(t *testing.T) {
	if os.Getenv("KML_PROBE_DENIAL_TEST") != "capless-local" {
		t.Skip("requires an explicitly selected capless sandbox")
	}
	header := unix.CapUserHeader{Version: unix.LINUX_CAPABILITY_VERSION_3}
	var caps [2]unix.CapUserData
	if unix.Capget(&header, &caps[0]) != nil {
		t.Fatal("cannot verify capability boundary")
	}
	for _, c := range caps {
		if c.Effective != 0 || c.Permitted != 0 || c.Inheritable != 0 {
			t.Fatal("probe denial test requires zero capabilities")
		}
	}
	noNew, err := unix.PrctlRetInt(unix.PR_GET_NO_NEW_PRIVS, 0, 0, 0, 0)
	if err != nil || noNew != 1 {
		t.Fatal("no-new-privileges is required")
	}
	seccomp, err := unix.PrctlRetInt(unix.PR_GET_SECCOMP, 0, 0, 0, 0)
	if err != nil || seccomp != 2 {
		t.Fatal("filtered seccomp is required")
	}
	for _, count := range []int{1, 2} {
		outcomes := make(chan error, count)
		for i := 0; i < count; i++ {
			go func() { outcomes <- probeProgramme(ebpf.TracePoint, nil) }()
		}
		for i := 0; i < count; i++ {
			err := <-outcomes
			if !errors.Is(err, unix.EPERM) && !errors.Is(err, unix.EACCES) {
				t.Fatal("fixed tracepoint load was not denied by policy")
			}
		}
	}
}
