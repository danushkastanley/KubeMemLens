/* Native test-only kernel/helper surface. Never included in a BPF build. */
#include <stddef.h>
#include <stdint.h>
#include <string.h>
#include <stdbool.h>

typedef uint64_t __u64;
typedef uint32_t __u32;
#undef __always_inline
#define __always_inline inline __attribute__((always_inline))
#define __uint(name, value) int (*name)[value]
#define __type(name, type) type *name
#define SEC(section)
#define BPF_PROG(name, ...) name(__VA_ARGS__)
#define BPF_MAP_TYPE_HASH 1
#define BPF_ANY 0
#define bpf_core_read(destination, size, source) read_kernel(destination, size, source)

struct kernfs_node { __u64 id; };
struct cgroup { struct kernfs_node *kn; };
struct css_set { struct cgroup *dfl_cgrp; };
struct task_struct { struct css_set *cgroups; int pid; char comm[16]; };
struct mem_cgroup { int unused; };
struct oom_control { struct mem_cgroup *memcg; int order; };
struct kml_counts { __u64 produced, sampled, lost, rejected; };

static __u64 target_cgroup = 123, deadline_ns = 1000, event_limit = 10000;
static int counts, events, control, target_ref;
static int read_kernel(void *, size_t, const void *);
static void *bpf_map_lookup_elem(const void *, const void *);
static int bpf_map_delete_elem(const void *, const void *);
static int bpf_map_update_elem(const void *, const void *, const void *, int);
static int bpf_current_task_under_cgroup(const void *, int);
static __u64 bpf_get_current_pid_tgid(void);
static __u64 bpf_ktime_get_ns(void);
static void *bpf_ringbuf_reserve(const void *, size_t, int);
static void bpf_ringbuf_submit(void *, int);
static struct kml_counts *candidate(void);
