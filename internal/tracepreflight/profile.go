// Package tracepreflight owns the bounded, read-only prototype preflight contract.
// Kernel adapters live outside the standard application's module and commands.
package tracepreflight

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

const (
	SchemaVersion  = 1
	MaxChecks      = 24
	MaxValueBytes  = 128
	MaxReportBytes = 16 << 10
)

type ID string

const (
	Slot             ID = "preflight_slot"
	Platform         ID = "platform"
	Architecture     ID = "architecture"
	Kernel           ID = "kernel"
	CgroupV2         ID = "cgroup_v2"
	BTF              ID = "btf"
	Capabilities     ID = "capabilities"
	SecurityPolicy   ID = "security_policy"
	LSM              ID = "lsm"
	BPFFS            ID = "bpffs"
	EngineIdentity   ID = "engine_identity"
	Ownership        ID = "ownership"
	ProbeCleanup     ID = "probe_cleanup"
	Kprobe           ID = "programme_kprobe"
	Tracepoint       ID = "programme_tracepoint"
	CgroupHelper     ID = "helper_current_cgroup"
	KernelReadHelper ID = "helper_probe_read_kernel"
	BootTimeHelper   ID = "helper_boot_time"
	RingBuffer       ID = "map_ring_buffer"
	FileHooks        ID = "file_hooks"
	CacheHooks       ID = "cache_hooks"
	OOMHook          ID = "oom_hook"
)

// Profile is an immutable selection made by the prototype build, not a request.
// The initial baseline has no incident programme approval or runtime allowlist.
type Profile struct {
	Version        int    `json:"version"`
	Name           string `json:"name"`
	EngineVersion  string `json:"engineVersion"`
	EngineDigest   string `json:"engineDigest"`
	Scope          string `json:"scope"`
	InventoryScope string `json:"inventoryScope"`
	Checks         []ID   `json:"checks"`
}

func Baseline() Profile {
	return Profile{
		Version: 1, Name: "inspektor-baseline-v1", EngineVersion: "v0.56.0",
		EngineDigest:   "sha256:bdb8f3ee121d94736570f6554b07d86022d887db9d6e9b9da20f7db31e32a7a1",
		Scope:          "engine-baseline-only",
		InventoryScope: "current-and-recorded-worker-descriptors",
		Checks: []ID{Platform, Architecture, Kernel, CgroupV2, BTF, Capabilities,
			SecurityPolicy, LSM, BPFFS, EngineIdentity, Ownership, Kprobe, Tracepoint,
			CgroupHelper, KernelReadHelper, BootTimeHelper, RingBuffer, FileHooks, CacheHooks, OOMHook, ProbeCleanup},
	}
}

func (p Profile) Digest() string {
	data, _ := json.Marshal(p)
	digest := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(digest[:])
}

func needsKernelProbe(id ID) bool {
	switch id {
	case Kprobe, Tracepoint, CgroupHelper, KernelReadHelper, BootTimeHelper, RingBuffer:
		return true
	default:
		return false
	}
}
