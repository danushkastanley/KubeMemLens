package probe

import (
	"errors"
	"os"
	"runtime"
	"strings"
	"testing"

	"github.com/cilium/ebpf"
	p "github.com/danushkastanley/kube-memlens/internal/tracepreflight"
	"golang.org/x/sys/unix"
)

func fixtureFiles(files map[string]string) func(string, int64) ([]byte, error) {
	return func(path string, max int64) ([]byte, error) {
		value, ok := files[path]
		if !ok {
			return nil, os.ErrNotExist
		}
		if int64(len(value)) > max {
			return nil, errors.New("too large")
		}
		return []byte(value), nil
	}
}

func TestCapabilityAndPolicyFailuresHaveDistinctReasons(t *testing.T) {
	for _, tt := range []struct {
		status       string
		caps, policy p.Reason
	}{
		{"CapEff:\t0000000000000000\nNoNewPrivs:\t1\nSeccomp:\t2\n", p.CapabilityMissing, p.Available},
		{"CapEff:\t000000c000000000\nNoNewPrivs:\t1\nSeccomp:\t2\n", p.Available, p.Available},
		{"CapEff:\t000000c000200000\nNoNewPrivs:\t1\nSeccomp:\t2\n", p.ExcessPrivilege, p.Available},
		{"CapEff:\t000000c000000000\nNoNewPrivs:\t0\nSeccomp:\t2\n", p.Available, p.PolicyDenied},
		{"CapEff:\t000000c000000000\nNoNewPrivs:\t1\nSeccomp:\t0\n", p.Available, p.PolicyDenied},
		{"CapEff:\tinvalid\n", p.ProbeFailed, p.PolicyUnreported},
	} {
		status := tt.status + "CapPrm:\t000000c000000000\nCapBnd:\t000000c000000000\nCapInh:\t0000000000000000\nCapAmb:\t0000000000000000\n"
		local := &Local{read: fixtureFiles(map[string]string{"/proc/self/status": status})}
		if got := local.capabilities(); got.Reason != tt.caps {
			t.Fatalf("capability reason %s", got.Reason)
		}
		if got := local.policy(); got.Reason != tt.policy {
			t.Fatalf("policy reason %s", got.Reason)
		}
	}
}

func TestKernelAndArchitectureAreObservedRatherThanAssumed(t *testing.T) {
	for _, tt := range []struct {
		release string
		reason  p.Reason
	}{
		{"5.9.9", p.KernelUnsupported}, {"5.10.0-vendor", p.Available}, {"7.0.12-linuxkit", p.Available}, {"unknown", p.ProbeFailed},
	} {
		local := &Local{uname: func() (string, string, error) { return tt.release, "aarch64", nil }}
		if got := local.kernel(); got.Reason != tt.reason {
			t.Fatalf("%s: %s", tt.release, got.Reason)
		}
	}
	local := &Local{uname: func() (string, string, error) {
		return "7.0.0", map[string]string{"arm64": "aarch64", "amd64": "x86_64"}[runtime.GOARCH], nil
	}}
	if got := local.architecture(); got.State != p.Supported {
		t.Fatal("native architecture rejected")
	}
	local.uname = func() (string, string, error) { return "7.0.0", "other-machine", nil }
	if got := local.architecture(); got.Reason != p.ArchitectureUnsupported {
		t.Fatal("unknown architecture accepted")
	}
}

func TestBTFMissingAndMalformedAreDistinct(t *testing.T) {
	local := &Local{read: fixtureFiles(nil)}
	if got := local.btf(); got.Reason != p.BTFMissing {
		t.Fatal("missing BTF misclassified")
	}
	local.read = fixtureFiles(map[string]string{"/sys/kernel/btf/vmlinux": "invalid"})
	if got := local.btf(); got.Reason != p.BTFInvalid {
		t.Fatal("invalid BTF accepted")
	}
}

func TestHooksDoNotReturnUnrelatedKernelSymbols(t *testing.T) {
	local := &Local{read: fixtureFiles(map[string]string{
		"/sys/kernel/tracing/available_filter_functions": "private_symbol [private_module]\nvfs_read\nvfs_write\noom_kill_process\n",
		"/sys/kernel/tracing/available_events":           "filemap:mm_filemap_add_to_page_cache\nfilemap:mm_filemap_delete_from_page_cache\n",
	})}
	for _, id := range []p.ID{p.FileHooks, p.CacheHooks, p.OOMHook} {
		got := local.hooks(id)
		if got.State != p.Supported || strings.Contains(got.Value, "private") {
			t.Fatal("hook check failed or leaked a symbol")
		}
	}
	local.read = fixtureFiles(map[string]string{"/sys/kernel/tracing/available_filter_functions": "vfs_read\n"})
	if got := local.hooks(p.FileHooks); got.Reason != p.HookMissing {
		t.Fatal("missing hook accepted")
	}
	if got := local.hooks(p.CacheHooks); got.Reason != p.HookUnreported {
		t.Fatal("unreadable inventory reported absent")
	}
}

func TestSecurityPolicyAndUnsupportedHelperErrorsRemainDistinct(t *testing.T) {
	for _, tt := range []struct {
		err    error
		reason p.Reason
	}{
		{nil, p.Available}, {ebpf.ErrNotSupported, p.HelperMissing}, {unix.EACCES, p.PolicyDenied}, {unix.EPERM, p.PolicyDenied}, {errors.New("private verifier address"), p.ProbeFailed},
	} {
		got := featureResult(p.CgroupHelper, p.HelperMissing, tt.err)
		if got.Reason != tt.reason || got.Value != "" {
			t.Fatal("helper result misclassified or retained verifier output")
		}
	}
}

func TestFrozenReferenceInputsRejectSubstitution(t *testing.T) {
	local := New("../testdata/reference")
	if got := local.identity(); got.State != p.Supported {
		t.Fatalf("frozen inputs rejected: %s", got.Reason)
	}
	local.read = func(string, int64) ([]byte, error) { return []byte("substitution"), nil }
	if got := local.identity(); got.Reason != p.EngineMismatch {
		t.Fatal("substituted manifest accepted")
	}
}

func TestLSMInventoryDoesNotExposeLabelsOrAssumeMissingMeansDisabled(t *testing.T) {
	for _, tt := range []struct {
		files  map[string]string
		state  p.State
		reason p.Reason
	}{
		{map[string]string{"/sys/kernel/security/lsm": "capability,bpf,landlock"}, p.Supported, p.Available},
		{map[string]string{"/proc/self/attr/current": "private-tenant-label (enforce)"}, p.Supported, p.Available},
		{map[string]string{"/proc/self/attr/current": "private-tenant-label (complain)"}, p.Degraded, p.PolicyUnreported},
		{map[string]string{"/sys/kernel/security/lsm": "unknown-module"}, p.Degraded, p.PolicyUnreported},
		{nil, p.Degraded, p.PolicyUnreported},
	} {
		local := &Local{read: fixtureFiles(tt.files)}
		got := local.lsm()
		if got.State != tt.state || got.Reason != tt.reason || strings.Contains(got.Value, "private") {
			t.Fatalf("LSM result: %+v", got)
		}
	}
}

func TestCapabilityBoundingSetCannotHideExcessPrivilege(t *testing.T) {
	local := &Local{read: fixtureFiles(map[string]string{"/proc/self/status": "CapEff:\t000000c000000000\nCapPrm:\t000000c000000000\nCapBnd:\t000000c000200000\nCapInh:\t0\nCapAmb:\t0\n"})}
	if got := local.capabilities(); got.Reason != p.ExcessPrivilege {
		t.Fatal("latent SYS_ADMIN accepted")
	}
}
