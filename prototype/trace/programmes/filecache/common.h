/* SPDX-License-Identifier: (GPL-2.0-only OR BSD-2-Clause) */
#ifndef KML_FILECACHE_COMMON_H
#define KML_FILECACHE_COMMON_H

#include <vmlinux.h>
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_core_read.h>
#include <bpf/bpf_tracing.h>
#include <gadget/macros.h>

/* Installation-owned constants. All zeroes is a deny-all configuration. */
const volatile __u64 target_cgroup = 0;
const volatile __u64 deadline_ns = 0;
const volatile __u64 event_limit = 0;
GADGET_PARAM(target_cgroup);
GADGET_PARAM(deadline_ns);
GADGET_PARAM(event_limit);

struct kml_counts {
	__u64 produced;
	__u64 sampled;
	__u64 lost;
	__u64 rejected;
};

struct {
	__uint(type, BPF_MAP_TYPE_ARRAY);
	__uint(max_entries, 1);
	__type(key, __u32);
	__type(value, struct kml_counts);
} counts SEC(".maps");

struct {
	__uint(type, BPF_MAP_TYPE_RINGBUF);
	__uint(max_entries, 262144);
} events SEC(".maps");

/* Worker-owned activation. Attachments start with collection disabled. */
struct {
	__uint(type, BPF_MAP_TYPE_ARRAY);
	__uint(max_entries, 1);
	__uint(map_flags, BPF_F_RDONLY_PROG);
	__type(key, __u32);
	__type(value, __u32);
} control SEC(".maps");

/* Each attached programme retains this map, which retains the target cgroup. */
struct {
	__uint(type, BPF_MAP_TYPE_CGROUP_ARRAY);
	__uint(max_entries, 1);
	__uint(key_size, sizeof(__u32));
	__uint(value_size, sizeof(__u32));
} target_ref SEC(".maps");

/* No SDK resettable loss map or per-CPU heap: counters are cumulative. */
/* Return the hook's monotonic observation time, or zero outside the target. */
static __always_inline __u64 selected_time(void)
{
	if (!target_cgroup || !deadline_ns || !event_limit || event_limit > 100000)
		return 0;
	__u32 zero = 0;
	__u32 *enabled = bpf_map_lookup_elem(&control, &zero);
	if (!enabled || *enabled != 1)
		return 0;
	if (bpf_get_current_cgroup_id() != target_cgroup)
		return 0;
	/* Ancestor membership alone is insufficient: retain the exact-ID check. */
	if (bpf_current_task_under_cgroup(&target_ref, 0) != 1)
		return 0;
	__u64 now = bpf_ktime_get_ns();
	return now < deadline_ns ? now : 0;
}

static __always_inline struct kml_counts *candidate(void)
{
	__u32 zero = 0;
	struct kml_counts *c = bpf_map_lookup_elem(&counts, &zero);
	if (!c)
		return NULL;
	__u64 ordinal = __sync_fetch_and_add(&c->produced, 1);
	if (ordinal >= event_limit) {
		__sync_fetch_and_add(&c->sampled, 1);
		return NULL;
	}
	return c;
}

char LICENSE[] SEC("license") = "Dual BSD/GPL";
#endif
