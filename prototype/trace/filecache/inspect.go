// Package filecache implements the fixed incident programme boundary.
package filecache

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sort"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/asm"
	"github.com/cilium/ebpf/btf"
	"github.com/danushkastanley/kube-memlens/internal/trace"
)

const MaxObjectBytes = 4 << 20

var ErrObject = errors.New("invalid file/cache programme object")

type MapInventory struct {
	Name       string `json:"name"`
	Type       string `json:"type"`
	KeyBytes   uint32 `json:"keyBytes"`
	ValueBytes uint32 `json:"valueBytes"`
	MaxEntries uint32 `json:"maxEntries"`
	Flags      uint32 `json:"flags"`
}
type HookInventory struct {
	Name         string   `json:"name"`
	Section      string   `json:"section"`
	Type         string   `json:"type"`
	License      string   `json:"license"`
	Instructions int      `json:"instructions"`
	Helpers      []string `json:"helpers"`
}
type ObjectInventory struct {
	SHA256     string          `json:"sha256"`
	ObjectSize int             `json:"objectBytes"`
	EventSize  uint32          `json:"eventBytes"`
	Maps       []MapInventory  `json:"maps"`
	Hooks      []HookInventory `json:"hooks"`
	Constants  []string        `json:"constants"`
}

// Inspect parses ELF/BTF in userspace only. It does not create kernel maps,
// load programmes, establish approval, or prove verifier/runtime compatibility.
func Inspect(kind trace.Kind, object []byte) (ObjectInventory, error) {
	if len(object) == 0 || len(object) > MaxObjectBytes || !validProgrammeKind(kind) {
		return ObjectInventory{}, ErrObject
	}
	spec, err := ebpf.LoadCollectionSpecFromReader(bytes.NewReader(object))
	if err != nil {
		return ObjectInventory{}, ErrObject
	}
	var event *btf.Struct
	name := "file_event"
	if kind == trace.Cache {
		name = "cache_event"
	}
	if kind == trace.OOM {
		name = "oom_event"
	}
	if err := spec.Types.TypeByName(name, &event); err != nil {
		return ObjectInventory{}, ErrObject
	}
	size, err := btf.Sizeof(event)
	if err != nil || size <= 0 || size > 1024 {
		return ObjectInventory{}, ErrObject
	}
	sum := sha256.Sum256(object)
	i := ObjectInventory{SHA256: hex.EncodeToString(sum[:]), ObjectSize: len(object), EventSize: uint32(size)}
	for name, m := range spec.Maps {
		i.Maps = append(i.Maps, MapInventory{name, m.Type.String(), m.KeySize, m.ValueSize, m.MaxEntries, m.Flags})
	}
	for name, p := range spec.Programs {
		seen := map[string]bool{}
		for _, ins := range p.Instructions {
			if ins.IsBuiltinCall() {
				if ins.Constant < 0 || ins.Constant > 1<<31-1 {
					return ObjectInventory{}, ErrObject
				}
				seen[asm.BuiltinFunc(ins.Constant).String()] = true
			}
		}
		helpers := make([]string, 0, len(seen))
		for helper := range seen {
			helpers = append(helpers, helper)
		}
		sort.Strings(helpers)
		i.Hooks = append(i.Hooks, HookInventory{name, p.SectionName, p.Type.String(), p.License, len(p.Instructions), helpers})
	}
	for name, v := range spec.Variables {
		if v.Constant() {
			i.Constants = append(i.Constants, name)
		}
	}
	sort.Slice(i.Maps, func(a, b int) bool { return i.Maps[a].Name < i.Maps[b].Name })
	sort.Slice(i.Hooks, func(a, b int) bool { return i.Hooks[a].Name < i.Hooks[b].Name })
	sort.Strings(i.Constants)
	return i, nil
}
