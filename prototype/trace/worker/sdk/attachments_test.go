package sdk

import (
	"testing"

	"github.com/cilium/ebpf"
	ebpfoperator "github.com/inspektor-gadget/inspektor-gadget/pkg/operators/ebpf"
)

func TestConstrainedAttachmentVocabulary(t *testing.T) {
	for _, p := range []*ebpf.ProgramSpec{
		{Type: ebpf.TracePoint, SectionName: "tracepoint/filemap/mm_filemap_add_to_page_cache", AttachTo: "filemap/mm_filemap_add_to_page_cache"},
		{Type: ebpf.Tracing, AttachType: ebpf.AttachTraceFEntry, SectionName: "fentry/vfs_read", AttachTo: "vfs_read"},
		{Type: ebpf.Tracing, AttachType: ebpf.AttachTraceFExit, SectionName: "fexit/oom_kill_process", AttachTo: "oom_kill_process"},
	} {
		if err := ebpfoperator.ValidateConstrainedAttachment(p); err != nil {
			t.Fatalf("supported %s rejected: %v", p.SectionName, err)
		}
	}
	for _, p := range []*ebpf.ProgramSpec{
		nil, {},
		{Type: ebpf.TracePoint, SectionName: "tracepoint/filemap", AttachTo: "filemap"},
		{Type: ebpf.TracePoint, SectionName: "tracepoint/filemap/", AttachTo: "filemap/"},
		{Type: ebpf.TracePoint, SectionName: "tracepoint//event", AttachTo: "/event"},
		{Type: ebpf.TracePoint, SectionName: "tracepoint/a/b/c", AttachTo: "a/b/c"},
		{Type: ebpf.TracePoint, SectionName: "tracepoint/other/event", AttachTo: "filemap/event"},
		{Type: ebpf.TracePoint, AttachType: ebpf.AttachTraceFEntry, SectionName: "tracepoint/filemap/event", AttachTo: "filemap/event"},
		{Type: ebpf.Tracing, AttachType: ebpf.AttachTraceFExit, SectionName: "fentry/vfs_read", AttachTo: "vfs_read"},
		{Type: ebpf.Tracing, AttachType: ebpf.AttachTraceRawTp, SectionName: "tp_btf/event", AttachTo: "event"},
		{Type: ebpf.Tracing, AttachType: ebpf.AttachTraceIter, SectionName: "iter/task", AttachTo: "task"},
	} {
		if err := ebpfoperator.ValidateConstrainedAttachment(p); err == nil {
			t.Fatalf("unsupported attachment accepted: %+v", p)
		}
	}
	for _, kind := range []ebpf.ProgramType{ebpf.Kprobe, ebpf.SocketFilter, ebpf.SchedCLS, ebpf.SockOps, ebpf.SkSKB, ebpf.SkMsg, ebpf.LSM, ebpf.PerfEvent, ebpf.RawTracepoint, ebpf.XDP} {
		p := &ebpf.ProgramSpec{Type: kind, SectionName: "uprobe/example", AttachTo: "example"}
		if err := ebpfoperator.ValidateConstrainedAttachment(p); err == nil {
			t.Fatalf("unsupported programme class %s accepted", kind)
		}
	}
}

func TestVerifiedCachePreparationWithoutReaders(t *testing.T) {
	verifyObjectPreparation(t, "cache", "KML_FILECACHE_OBJECTS")
}
