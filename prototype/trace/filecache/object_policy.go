package filecache

import (
	"bytes"
	"encoding/binary"

	"github.com/cilium/ebpf"
	"github.com/danushkastanley/kube-memlens/internal/trace"
)

// ValidateObject supplements the signed digest with the fixed capability and
// resource vocabulary. It is a userspace check, not a proof of kernel filtering.
func ValidateObject(kind trace.Kind, object []byte) error {
	i, err := Inspect(kind, object)
	if err != nil {
		return err
	}
	spec, err := ebpf.LoadCollectionSpecFromReader(bytes.NewReader(object))
	if err != nil || spec.ByteOrder != binary.LittleEndian {
		return ErrObject
	}
	return validatePolicy(kind, i, spec)
}

func validatePolicy(kind trace.Kind, i ObjectInventory, spec *ebpf.CollectionSpec) error {
	hooks := map[string]string{"cache_add": "tracepoint/filemap/mm_filemap_add_to_page_cache", "cache_remove": "tracepoint/filemap/mm_filemap_delete_from_page_cache"}
	eventSize, rodata, globals := uint32(24), uint32(24), uint32(32)
	if kind == trace.Files {
		hooks = map[string]string{"read_begin": "fentry/vfs_read", "write_begin": "fentry/vfs_write", "read_end": "fexit/vfs_read", "write_end": "fexit/vfs_write", "path_permission": "fexit/security_file_permission"}
		eventSize, rodata, globals = 552, 568, 40
	}
	if i.EventSize != eventSize || len(i.Hooks) != len(hooks) {
		return ErrObject
	}
	for _, hook := range i.Hooks {
		if hooks[hook.Name] != hook.Section || hook.License != "Dual BSD/GPL" || hook.Instructions > 4096 {
			return ErrObject
		}
		if (kind == trace.Files && hook.Type != "Tracing") || (kind == trace.Cache && hook.Type != "TracePoint") {
			return ErrObject
		}
		for _, helper := range hook.Helpers {
			switch helper {
			case "FnGetCurrentCgroupId", "FnCurrentTaskUnderCgroup", "FnKtimeGetNs", "FnMapLookupElem", "FnRingbufReserve", "FnRingbufSubmit", "FnProbeReadKernel":
			case "FnDPath":
				if hook.Name != "path_permission" {
					return ErrObject
				}
			case "FnGetCurrentPidTgid", "FnMapDeleteElem", "FnMapUpdateElem":
				if kind != trace.Files {
					return ErrObject
				}
			default:
				return ErrObject
			}
		}
	}
	maps := map[string]MapInventory{
		"events":     {"events", "RingBuf", 0, 0, 262144, 0},
		"counts":     {"counts", "Array", 4, 32, 1, 0},
		"control":    {"control", "Array", 4, 4, 1, 128}, // BPF_F_RDONLY_PROG
		"target_ref": {"target_ref", "CGroupArray", 4, 4, 1, 0},
		".rodata":    {".rodata", "Array", 4, rodata, 1, 128},
		".bss":       {".bss", "Array", 4, globals, 1, 0},
	}
	if kind == trace.Files {
		maps["paths"] = MapInventory{"paths", "Hash", 8, 536, 256, 0}
	}
	if len(i.Maps) != len(maps) {
		return ErrObject
	}
	for _, m := range i.Maps {
		if expected, ok := maps[m.Name]; !ok || m != expected || spec.Maps[m.Name].Pinning != ebpf.PinNone {
			return ErrObject
		}
	}
	for _, name := range []string{"target_cgroup", "deadline_ns", "event_limit"} {
		v := spec.Variables[name]
		var value uint64
		if v == nil || !v.Constant() || v.Get(&value) != nil || value != 0 {
			return ErrObject
		}
	}
	if kind == trace.Files {
		v := spec.Variables["path_bytes"]
		var value uint32
		if v == nil || !v.Constant() || v.Get(&value) != nil || value != 0 {
			return ErrObject
		}
	}
	if len(spec.Maps["control"].Contents) != 0 {
		return ErrObject
	}
	return nil
}
