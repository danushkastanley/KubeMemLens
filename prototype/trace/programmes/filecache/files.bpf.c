/* SPDX-License-Identifier: (GPL-2.0-only OR BSD-2-Clause) */
#include "common.h"

/* Zero omits paths; confirmed requests may select at most 512 original bytes. */
const volatile __u32 path_bytes = 0;
GADGET_PARAM(path_bytes);

struct file_event {
	__u64 monotonic_ns;
	__u64 requested;
	__u64 completed;
	__u32 operation; /* 1 read, 2 write; only successful regular-file VFS calls */
	__u32 path_length;
	char path[513];
};
/* Preserve the ABI type without enabling the SDK's generic event reader. */
const struct file_event *kml_file_event_type __attribute__((used));

struct pending_path {
	__u64 file;
	__u32 operation;
	__u32 length;
	char path[513];
};
static const struct pending_path empty_path = {};

/* The caller has zeroed the event and validated length <= 512. d_path can
 * leave its backwards-built string beyond the returned prefix after memmove.
 * Copy only declared bytes; never export that helper scratch area. */
static __always_inline void copy_path_prefix(char *destination,
					   const char *source, __u32 length)
{
#pragma clang loop unroll(disable)
	for (__u32 i = 0; i < 512; i++) {
		if (i >= length)
			break;
		destination[i] = source[i];
	}
}

struct {
	__uint(type, BPF_MAP_TYPE_HASH);
	__uint(max_entries, 256);
	__type(key, __u64);
	__type(value, struct pending_path);
} paths SEC(".maps");

static __always_inline bool regular_file(struct file *file)
{
	return file && (BPF_CORE_READ(file, f_inode, i_mode) & 00170000) == 0100000;
}

static __always_inline int begin(struct file *file, __u32 operation)
{
	if (!selected_time() || !path_bytes || path_bytes > 512)
		return 0;
	if (!regular_file(file))
		return 0;
	__u64 tid = bpf_get_current_pid_tgid();
	/* Delete first so a failed replacement cannot reuse an earlier path. */
	bpf_map_delete_elem(&paths, &tid);
	if (bpf_map_update_elem(&paths, &tid, &empty_path, BPF_NOEXIST))
		return 0; /* The matching exit counts the missing context as rejected. */
	struct pending_path *p = bpf_map_lookup_elem(&paths, &tid);
	if (!p)
		return 0;
	p->file = (__u64)file;
	p->operation = operation;
	return 0;
}

SEC("fexit/security_file_permission")
int BPF_PROG(path_permission, struct file *file, int mask, int ret)
{
	if (!selected_time() || !path_bytes || path_bytes > 512 || ret)
		return 0;
	__u64 tid = bpf_get_current_pid_tgid();
	struct pending_path *p = bpf_map_lookup_elem(&paths, &tid);
	if (!p || p->file != (__u64)file)
		return 0;
	p->length = 0;
	__builtin_memset(p->path, 0, sizeof(p->path));
	/* d_path uses the current task's root, and this hook is helper-allowlisted. */
	long length = bpf_d_path(&file->f_path, p->path, path_bytes + 1);
	if (length > 1 && length <= path_bytes + 1)
		p->length = length - 1;
	return 0;
}

static __always_inline int finish(struct file *file, __u64 requested,
				 long completed, __u32 operation)
{
	__u64 tid = bpf_get_current_pid_tgid();
	__u64 observed = selected_time();
	if (!observed || !regular_file(file))
		goto discard;
	struct kml_counts *c = candidate();
	if (!c)
		goto discard;
	if (completed < 0 || (__u64)completed > requested || path_bytes > 512)
		goto rejected;
	struct pending_path *p = NULL;
	__u32 length = 0;
	if (path_bytes) {
		p = bpf_map_lookup_elem(&paths, &tid);
		if (!p || p->file != (__u64)file || p->operation != operation)
			goto rejected;
		length = p->length;
		if (!length || length > path_bytes)
			goto rejected;
	}
	struct file_event *e = bpf_ringbuf_reserve(&events, sizeof(*e), 0);
	if (!e) {
		__sync_fetch_and_add(&c->lost, 1);
		goto discard;
	}
	__builtin_memset(e, 0, sizeof(*e));
	e->monotonic_ns = observed;
	e->requested = requested;
	e->completed = completed;
	e->operation = operation;
	if (p) {
		e->path_length = length;
		copy_path_prefix(e->path, p->path, length);
	}
	bpf_ringbuf_submit(e, 0);
	goto discard;
rejected:
	__sync_fetch_and_add(&c->rejected, 1);
discard:
	if (path_bytes)
		bpf_map_delete_elem(&paths, &tid);
	return 0;
}

SEC("fentry/vfs_read")
int BPF_PROG(read_begin, struct file *file)
{
	return begin(file, 1);
}
SEC("fentry/vfs_write")
int BPF_PROG(write_begin, struct file *file)
{
	return begin(file, 2);
}
SEC("fexit/vfs_read")
int BPF_PROG(read_end, struct file *file, char *buffer, size_t count,
	     loff_t *position, ssize_t ret)
{
	return finish(file, count, ret, 1);
}
SEC("fexit/vfs_write")
int BPF_PROG(write_end, struct file *file, const char *buffer, size_t count,
	     loff_t *position, ssize_t ret)
{
	return finish(file, count, ret, 2);
}
