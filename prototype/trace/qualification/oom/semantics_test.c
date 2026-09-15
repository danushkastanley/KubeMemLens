/* Exercise the exact production hook functions against native test helpers. */
#include <assert.h>
#include <stdio.h>
#include "oom.bpf.c"

static struct kernfs_node selected_node = {123}, other_node = {456};
static struct cgroup selected_group = {&selected_node}, other_group = {&other_node};
static struct css_set selected_css = {&selected_group}, other_css = {&other_group};
static struct task_struct selected = {&selected_css, 1234, "fixture"};
static struct task_struct other = {&other_css, 2345, "other-fixture"};
static struct mem_cgroup memcg;
static struct oom_control decision = {&memcg, 0};
static struct oom_context decision_context, kill_context;
static struct oom_event record;
static struct kml_counts totals;
static bool have_decision, have_kill, ring_available, fail_kill_map, fail_command;
static __u32 enabled;
static __u64 now;
static unsigned int emissions, other_private_reads;

static int read_kernel(void *destination, size_t size, const void *source)
{
	if (source == &other.pid || source == &other.comm)
		other_private_reads++;
	if (fail_command && source == &selected.comm)
		return -1;
	memcpy(destination, source, size);
	return 0;
}
static void *bpf_map_lookup_elem(const void *map, const void *key)
{
	(void)key;
	if (map == &control) return &enabled;
	if (map == &decisions) return have_decision ? &decision_context : NULL;
	if (map == &kills) return have_kill ? &kill_context : NULL;
	assert(map == &counts);
	return &totals;
}
static int bpf_map_delete_elem(const void *map, const void *key)
{
	assert(*((const __u64 *)key) == 999);
	if (map == &decisions) have_decision = false;
	else { assert(map == &kills); have_kill = false; }
	return 0;
}
static int bpf_map_update_elem(const void *map, const void *key, const void *value, int flags)
{
	assert(*((const __u64 *)key) == 999 && flags == BPF_ANY);
	if (map == &decisions) {
		decision_context = *((const struct oom_context *)value);
		have_decision = true;
	} else {
		assert(map == &kills);
		if (fail_kill_map) return -1;
		kill_context = *((const struct oom_context *)value);
		have_kill = true;
	}
	return 0;
}
static int bpf_current_task_under_cgroup(const void *map, int index)
{
	assert(map == &target_ref && index == 0);
	return 0; /* The invoking task is deliberately outside the selected cgroup. */
}
static __u64 bpf_get_current_pid_tgid(void) { return 999; }
static __u64 bpf_ktime_get_ns(void) { return now; }
static void *bpf_ringbuf_reserve(const void *map, size_t size, int flags)
{
	assert(map == &events && size == sizeof(record) && flags == 0);
	return ring_available ? &record : NULL;
}
static void bpf_ringbuf_submit(void *data, int flags)
{
	assert(data == &record && flags == 0);
	emissions++;
}
static struct kml_counts *candidate(void)
{
	if (totals.produced++ >= event_limit) { totals.sampled++; return NULL; }
	return &totals;
}
static void reset(void)
{
	have_decision = have_kill = fail_kill_map = fail_command = false;
	ring_available = true;
	enabled = 1;
	now = 100;
	emissions = other_private_reads = 0;
	memset(&record, 0, sizeof(record));
	memset(&totals, 0, sizeof(totals));
	decision.memcg = &memcg;
	decision.order = 0;
}
static void begin(void) { decision_begin(&decision); kill_begin(); }

int main(void)
{
	reset(); begin(); victim_marked(&selected);
	assert(emissions == 1 && record.scope == 1 && record.pid == 1234);
	assert(record.context_flags == 3 && !strcmp(record.command, "fixture"));
	assert(record.command[15] == 0 && record.reserved == 0);
	kill_end(); decision_end();
	assert(!have_kill && !have_decision);

	reset(); begin(); victim_marked(&other);
	assert(emissions == 0 && totals.produced == 0 && other_private_reads == 0);
	reset(); decision_begin(&decision); victim_marked(&selected);
	assert(emissions == 0 && totals.rejected == 1); /* Already-exiting path. */
	reset(); kill_begin(); victim_marked(&selected);
	assert(emissions == 1 && record.scope == 0); /* Missing decision context. */
	reset(); decision.memcg = NULL; begin(); victim_marked(&selected);
	assert(emissions == 1 && record.scope == 2);
	reset(); decision.order = -1; begin(); victim_marked(&selected);
	assert(emissions == 1 && record.scope == 0); /* Forced SysRq is not pressure proof. */
	reset(); fail_kill_map = true; begin(); victim_marked(&selected);
	assert(emissions == 0 && totals.rejected == 1);
	reset(); begin(); ring_available = false; victim_marked(&selected);
	assert(emissions == 0 && totals.produced == 1 && totals.lost == 1);
	reset(); begin(); fail_command = true; victim_marked(&selected);
	assert(emissions == 1 && record.context_flags == 1 && record.command[0] == 0);
	reset(); begin(); now = deadline_ns; victim_marked(&selected);
	assert(emissions == 0 && totals.produced == 0);
	reset(); begin(); enabled = 0; victim_marked(&selected); kill_end(); decision_end();
	assert(emissions == 0 && totals.produced == 0 && !have_kill && !have_decision);
	puts("Passed native OOM hook semantics; no BPF was loaded.");
	return 0;
}
