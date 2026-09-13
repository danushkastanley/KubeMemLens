package probe

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/cilium/ebpf/btf"
	p "github.com/danushkastanley/kube-memlens/internal/tracepreflight"
	"golang.org/x/sys/unix"
)

const allowedCapabilities = uint64(1<<unix.CAP_BPF | 1<<unix.CAP_PERFMON)

func (l *Local) kernel() p.Check {
	value, _, err := l.uname()
	if err != nil || len(value) > p.MaxValueBytes {
		return result(p.Kernel, p.Degraded, p.ProbeFailed, "")
	}
	base, _, _ := strings.Cut(value, "-")
	parts := strings.Split(base, ".")
	if len(parts) < 2 {
		return result(p.Kernel, p.Degraded, p.ProbeFailed, "")
	}
	major, e1 := strconv.Atoi(parts[0])
	minor, e2 := strconv.Atoi(parts[1])
	if e1 != nil || e2 != nil || major < 0 || minor < 0 {
		return result(p.Kernel, p.Degraded, p.ProbeFailed, "")
	}
	if major < 5 || major == 5 && minor < 10 {
		return result(p.Kernel, p.Unsupported, p.KernelUnsupported, value)
	}
	return result(p.Kernel, p.Supported, p.Available, value)
}

func (l *Local) architecture() p.Check {
	_, machine, err := l.uname()
	arch := map[string]string{"x86_64": "amd64", "aarch64": "arm64"}[machine]
	if err != nil || arch == "" || arch != runtime.GOARCH {
		return result(p.Architecture, p.Unsupported, p.ArchitectureUnsupported, "")
	}
	return result(p.Architecture, p.Supported, p.Available, arch)
}

func (l *Local) cgroup() p.Check {
	magic, err := l.statfs("/sys/fs/cgroup")
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return result(p.CgroupV2, p.Degraded, p.ProbeFailed, "")
	}
	if err != nil || magic != unix.CGROUP2_SUPER_MAGIC {
		return result(p.CgroupV2, p.Unsupported, p.CgroupMissing, "")
	}
	data, err := l.read("/sys/fs/cgroup/cgroup.controllers", 4096)
	if err != nil {
		return result(p.CgroupV2, p.Degraded, p.ProbeFailed, "")
	}
	for _, controller := range strings.Fields(string(data)) {
		if controller == "memory" {
			return result(p.CgroupV2, p.Supported, p.Available, "v2-memory")
		}
	}
	return result(p.CgroupV2, p.Unsupported, p.CgroupMissing, "")
}

func (l *Local) btf() p.Check {
	data, err := l.read("/sys/kernel/btf/vmlinux", 32<<20)
	if errors.Is(err, os.ErrNotExist) {
		return result(p.BTF, p.Unsupported, p.BTFMissing, "")
	}
	if err != nil {
		return result(p.BTF, p.Degraded, p.BTFUnreported, "")
	}
	if _, err := btf.LoadSpecFromReader(bytes.NewReader(data)); err != nil {
		return result(p.BTF, p.Unsupported, p.BTFInvalid, "")
	}
	return result(p.BTF, p.Supported, p.Available, "vmlinux")
}

func (l *Local) statusValue(key string) (string, bool) {
	data, err := l.read("/proc/self/status", 32<<10)
	if err != nil {
		return "", false
	}
	for _, line := range strings.Split(string(data), "\n") {
		name, value, ok := strings.Cut(line, ":")
		if ok && name == key {
			return strings.TrimSpace(value), true
		}
	}
	return "", false
}

func (l *Local) capabilities() p.Check {
	for _, field := range []string{"CapEff", "CapPrm", "CapBnd", "CapInh", "CapAmb"} {
		text, ok := l.statusValue(field)
		bits, err := strconv.ParseUint(text, 16, 64)
		if !ok || err != nil {
			return result(p.Capabilities, p.Degraded, p.ProbeFailed, "")
		}
		if bits & ^allowedCapabilities != 0 {
			return result(p.Capabilities, p.Unsupported, p.ExcessPrivilege, "")
		}
		if field == "CapEff" && bits&allowedCapabilities != allowedCapabilities {
			return result(p.Capabilities, p.Unsupported, p.CapabilityMissing, "")
		}
	}
	return result(p.Capabilities, p.Supported, p.Available, "BPF,PERFMON")
}

func (l *Local) policy() p.Check {
	noNewPrivileges, ok := l.statusValue("NoNewPrivs")
	seccomp, known := l.statusValue("Seccomp")
	if !ok || !known {
		return result(p.SecurityPolicy, p.Degraded, p.PolicyUnreported, "")
	}
	if noNewPrivileges != "1" || seccomp != "2" {
		return result(p.SecurityPolicy, p.Unsupported, p.PolicyDenied, "")
	}
	// These are policy prerequisites, not proof of syscall permission. The
	// subsequent programme/helper probes distinguish a policy denial from support.
	return result(p.SecurityPolicy, p.Supported, p.Available, "no-new-privileges;seccomp-filter")
}

func (l *Local) bpffs() p.Check {
	magic, err := l.statfs("/sys/fs/bpf")
	if err != nil || magic != unix.BPF_FS_MAGIC {
		return result(p.BPFFS, p.Degraded, p.BPFFSAbsent, "")
	}
	return result(p.BPFFS, p.Supported, p.Available, "pins-disabled")
}

func (l *Local) lsm() p.Check {
	data, err := l.read("/sys/kernel/security/lsm", 1024)
	if err == nil {
		known := map[string]bool{"capability": true, "landlock": true, "lockdown": true, "yama": true, "integrity": true, "apparmor": true, "selinux": true, "bpf": true, "smack": true, "tomoyo": true, "safesetid": true, "ipe": true}
		for _, name := range strings.Split(strings.TrimSpace(string(data)), ",") {
			if !known[name] {
				return result(p.LSM, p.Degraded, p.PolicyUnreported, "")
			}
		}
		return result(p.LSM, p.Supported, p.Available, "kernel-policy-inventory-present")
	}
	data, err = l.read("/proc/self/attr/current", 1024)
	if err == nil && strings.Contains(string(data), "(enforce)") {
		return result(p.LSM, p.Supported, p.Available, "process-profile-enforcing")
	}
	// An unreadable label is not proof that LSM enforcement is absent. Actual
	// verifier probes still test the operations under the current security policy.
	return result(p.LSM, p.Degraded, p.PolicyUnreported, "")
}

func (l *Local) identity() p.Check {
	// These are verified reference inputs, never permission to execute a gadget.
	for _, artifact := range []struct{ name, digest string }{
		{"engine-index.json", p.Baseline().EngineDigest},
		{"trace-open-index.json", "sha256:af88c9222e9c880251ab2093de9bdc13929f62b3cd9900c1c5c7b0df3304b2fb"},
		{"top-file-index.json", "sha256:c0eb11996a0ebe92bcaacf4d05767d6bded1161b1ea3188d1e093e9360165592"},
		{"trace-oomkill-index.json", "sha256:733b90f011f7bea7f72ae9a954d49a31a94334832b1862f969d2eda2c9d62f0d"},
	} {
		data, err := l.read(filepath.Join(l.bundle, artifact.name), 16<<10)
		if err != nil {
			return result(p.EngineIdentity, p.Unsupported, p.EngineUnreported, "")
		}
		sum := sha256.Sum256(data)
		if "sha256:"+hex.EncodeToString(sum[:]) != artifact.digest {
			return result(p.EngineIdentity, p.Unsupported, p.EngineMismatch, "")
		}
	}
	return result(p.EngineIdentity, p.Supported, p.Available, "v0.56.0;reference-only")
}

func (l *Local) hooks(id p.ID) p.Check {
	var path string
	var required []string
	switch id {
	case p.FileHooks:
		path = "/sys/kernel/tracing/available_filter_functions"
		required = []string{"vfs_read", "vfs_write"}
	case p.OOMHook:
		path = "/sys/kernel/tracing/available_filter_functions"
		required = []string{"oom_kill_process"}
	case p.CacheHooks:
		path = "/sys/kernel/tracing/available_events"
		required = []string{"filemap:mm_filemap_add_to_page_cache", "filemap:mm_filemap_delete_from_page_cache"}
	}
	data, err := l.read(path, 8<<20)
	if err != nil {
		return result(id, p.Degraded, p.HookUnreported, "")
	}
	missing := map[string]bool{}
	for _, name := range required {
		missing[name] = true
	}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 1024), 4096)
	for scanner.Scan() {
		name, _, _ := strings.Cut(scanner.Text(), " ")
		delete(missing, name)
	}
	if scanner.Err() != nil {
		return result(id, p.Degraded, p.HookUnreported, "")
	}
	if len(missing) > 0 {
		return result(id, p.Unsupported, p.HookMissing, "")
	}
	return result(id, p.Supported, p.Available, "present-not-attached")
}
