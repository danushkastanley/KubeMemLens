/* SPDX-License-Identifier: (GPL-2.0-only OR BSD-2-Clause) */
#include "common.h"

struct cache_event {
	__u64 monotonic_ns;
	__u64 pages;
	__u32 operation; /* 1 actual addition, 2 actual removal in selected context */
	__u32 reserved;
};
/* The constrained worker owns raw ring reads and strict length validation. */
const struct cache_event *kml_cache_event_type __attribute__((used));

static __always_inline int observe(struct trace_event_raw_mm_filemap_op_page_cache *ctx,
				 __u32 operation)
{
	/* No PFN, inode, device or other task identity reaches the ring buffer. */
	__u64 observed = selected_time();
	if (!observed)
		return 0;
	struct kml_counts *c = candidate();
	if (!c)
		return 0;
	__u8 order = BPF_CORE_READ(ctx, order);
	if (order >= 63) {
		__sync_fetch_and_add(&c->rejected, 1);
		return 0;
	}
	struct cache_event *e = bpf_ringbuf_reserve(&events, sizeof(*e), 0);
	if (!e) {
		__sync_fetch_and_add(&c->lost, 1);
		return 0;
	}
	e->monotonic_ns = observed;
	e->pages = 1ULL << order;
	e->operation = operation;
	e->reserved = 0;
	bpf_ringbuf_submit(e, 0);
	return 0;
}

SEC("tracepoint/filemap/mm_filemap_add_to_page_cache")
int cache_add(struct trace_event_raw_mm_filemap_op_page_cache *ctx)
{
	return observe(ctx, 1);
}
SEC("tracepoint/filemap/mm_filemap_delete_from_page_cache")
int cache_remove(struct trace_event_raw_mm_filemap_op_page_cache *ctx)
{
	return observe(ctx, 2);
}
