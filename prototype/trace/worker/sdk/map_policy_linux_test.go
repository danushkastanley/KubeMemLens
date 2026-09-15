package sdk

import (
	"testing"

	"github.com/cilium/ebpf"
	"github.com/danushkastanley/kube-memlens/prototype/trace/filecache"
	"golang.org/x/sys/unix"
)

func TestLoadedMapFlags(t *testing.T) {
	for _, tc := range []struct {
		name string
		base uint32
	}{
		{".bss", 0}, {".rodata", unix.BPF_F_RDONLY_PROG},
		{"control", unix.BPF_F_RDONLY_PROG}, {"counts", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			expected := filecache.MapInventory{Name: tc.name, Type: "Array", KeyBytes: 4, ValueBytes: 40, MaxEntries: 1, Flags: tc.base}
			info := ebpf.MapInfo{Type: ebpf.Array, KeySize: 4, ValueSize: 40, MaxEntries: 1, Flags: tc.base}
			if !matchesLoadedMap(expected, &info) {
				t.Fatal("original map flags rejected")
			}
			info.Flags |= unix.BPF_F_MMAPABLE
			want := tc.name == ".bss" || tc.name == ".rodata"
			if matchesLoadedMap(expected, &info) != want {
				t.Fatal("MMAPABLE accepted outside the loader's data sections, or rejected within them")
			}
			info.Flags |= unix.BPF_F_WRONLY_PROG
			if matchesLoadedMap(expected, &info) {
				t.Fatal("unexpected flag accepted")
			}
			if tc.base != 0 {
				info.Flags = unix.BPF_F_MMAPABLE
				if matchesLoadedMap(expected, &info) {
					t.Fatal("missing read-only programme flag accepted")
				}
			}
		})
	}
}

func TestLoadedMapShape(t *testing.T) {
	expected := filecache.MapInventory{Name: ".bss", Type: "Array", KeyBytes: 4, ValueBytes: 40, MaxEntries: 1}
	for _, mutate := range []func(*ebpf.MapInfo){
		func(m *ebpf.MapInfo) { m.Type = ebpf.Hash },
		func(m *ebpf.MapInfo) { m.KeySize++ },
		func(m *ebpf.MapInfo) { m.ValueSize++ },
		func(m *ebpf.MapInfo) { m.MaxEntries++ },
	} {
		info := ebpf.MapInfo{Type: ebpf.Array, KeySize: 4, ValueSize: 40, MaxEntries: 1, Flags: unix.BPF_F_MMAPABLE}
		mutate(&info)
		if matchesLoadedMap(expected, &info) {
			t.Fatal("changed map shape accepted")
		}
	}
	if matchesLoadedMap(expected, nil) {
		t.Fatal("missing map metadata accepted")
	}
}
