/* SPDX-License-Identifier: (GPL-2.0-only OR BSD-2-Clause) */
#include "common.h"

struct oom_context {
	__u64 started_ns;
	__u32 scope; /* 0 unknown, 1 memcg, 2 non-memcg allocation decision */
	__u32 reserved;
};

/* Invoking-task keys and scope stay in bounded private kernel maps only. */
struct {
	__uint(type, BPF_MAP_TYPE_HASH);
	__uint(max_entries, 64);
	__type(key, __u64);
	__type(value, struct oom_context);
} decisions SEC(".maps");

struct {
	__uint(type, BPF_MAP_TYPE_HASH);
	__uint(max_entries, 64);
	__type(key, __u64);
	__type(value, struct oom_context);
} kills SEC(".maps");

struct oom_event {
	__u64 monotonic_ns;
	__u32 scope;
	__u32 pid;
	__u32 context_flags; /* bit 0 PID read, bit 1 complete command read */
	__u32 reserved;
	char command[16];
};
const struct oom_event *kml_oom_event_type __attribute__((used));

static __always_inline __u64 active_time(void)
{
	if (!target_cgroup || !deadline_ns || !event_limit || event_limit > 100000)
		return 0;
	__u32 zero = 0;
	__u32 *enabled = bpf_map_lookup_elem(&control, &zero);
	if (!enabled || *enabled != 1)
		return 0;
	/* Retain and validate the cgroup-array reference. The caller need not be
	 * in the target: only the victim's exact cgroup controls emission. */
	if (bpf_current_task_under_cgroup(&target_ref, 0) < 0)
		return 0;
	__u64 now = bpf_ktime_get_ns();
	return now < deadline_ns ? now : 0;
}

static __always_inline __u64 victim_cgroup(struct task_struct *task)
{
	struct css_set *css = NULL;
	struct cgroup *group = NULL;
	struct kernfs_node *node = NULL;
	__u64 id = 0;
	if (!task || bpf_core_read(&css, sizeof(css), &task->cgroups) || !css)
		return 0;
	if (bpf_core_read(&group, sizeof(group), &css->dfl_cgrp) || !group)
		return 0;
	if (bpf_core_read(&node, sizeof(node), &group->kn) || !node)
		return 0;
	if (bpf_core_read(&id, sizeof(id), &node->id))
		return 0;
	return id;
}

SEC("fentry/oom_kill_process")
int BPF_PROG(decision_begin, struct oom_control *oc)
{
	__u64 key = bpf_get_current_pid_tgid();
	bpf_map_delete_elem(&decisions, &key);
	__u64 now = active_time();
	if (!now || !oc)
		return 0;
	struct oom_context context = {.started_ns = now};
	struct mem_cgroup *memcg = NULL;
	int order = 0;
	if (!bpf_core_read(&memcg, sizeof(memcg), &oc->memcg) &&
	    !bpf_core_read(&order, sizeof(order), &oc->order) && order != -1)
		context.scope = memcg ? 1 : 2;
	/* A missing map entry becomes unknown scope for a selected kill. */
	bpf_map_update_elem(&decisions, &key, &context, BPF_ANY);
	return 0;
}

SEC("fexit/oom_kill_process")
int BPF_PROG(decision_end)
{
	__u64 key = bpf_get_current_pid_tgid();
	bpf_map_delete_elem(&decisions, &key);
	return 0;
}

SEC("fentry/__oom_kill_process")
int BPF_PROG(kill_begin)
{
	__u64 key = bpf_get_current_pid_tgid();
	bpf_map_delete_elem(&kills, &key);
	__u64 now = active_time();
	if (!now)
		return 0;
	struct oom_context context = {.started_ns = now};
	struct oom_context *decision = bpf_map_lookup_elem(&decisions, &key);
	if (decision && decision->started_ns <= now && decision->scope <= 2)
		context.scope = decision->scope;
	bpf_map_update_elem(&kills, &key, &context, BPF_ANY);
	return 0;
}

SEC("fexit/__oom_kill_process")
int BPF_PROG(kill_end)
{
	__u64 key = bpf_get_current_pid_tgid();
	bpf_map_delete_elem(&kills, &key);
	return 0;
}

SEC("fentry/mark_oom_victim")
int BPF_PROG(victim_marked, struct task_struct *victim)
{
	__u64 now = active_time();
	if (!now || victim_cgroup(victim) != target_cgroup)
		return 0;
	struct kml_counts *c = candidate();
	if (!c)
		return 0;
	__u64 key = bpf_get_current_pid_tgid();
	struct oom_context *context = bpf_map_lookup_elem(&kills, &key);
	/* mark_oom_victim alone also covers tasks already exiting without a new
	 * kill. Only the paired kill path establishes the event below. */
	if (!context || context->started_ns > now || context->scope > 2) {
		__sync_fetch_and_add(&c->rejected, 1);
		return 0;
	}
	__u32 scope = context->scope;
	int pid = 0;
	char command[16] = {};
	__u32 flags = 0;
	if (!bpf_core_read(&pid, sizeof(pid), &victim->pid) && pid > 0)
		flags |= 1;
	else
		pid = 0;
	if (!bpf_core_read(command, sizeof(command), &victim->comm) && command[15] == 0)
		flags |= 2;
	if (victim_cgroup(victim) != target_cgroup) {
		__sync_fetch_and_add(&c->rejected, 1);
		return 0;
	}
	struct oom_event *event = bpf_ringbuf_reserve(&events, sizeof(*event), 0);
	if (!event) {
		__sync_fetch_and_add(&c->lost, 1);
		return 0;
	}
	__builtin_memset(event, 0, sizeof(*event));
	event->monotonic_ns = now;
	event->scope = scope;
	event->pid = pid;
	event->context_flags = flags;
	if (flags & 2) {
		for (int i = 0; i < 15; i++) {
			if (!command[i])
				break;
			event->command[i] = command[i];
		}
	}
	bpf_ringbuf_submit(event, 0);
	return 0;
}
