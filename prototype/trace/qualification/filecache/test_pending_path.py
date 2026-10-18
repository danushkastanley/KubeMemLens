"""Exercise the exact file-exit producer with bounded helper fixtures; no BPF load."""
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

PRODUCER = Path(__file__).resolve().parents[2] / 'programmes/filecache/files.bpf.c'
PREFIX = r'''
#include <assert.h>
#include <stdbool.h>
#include <stdint.h>
#include <string.h>
typedef uint32_t __u32;
typedef uint64_t __u64;
#ifndef __always_inline
#define __always_inline inline __attribute__((always_inline))
#endif
struct file { bool regular; } target = {true}, other = {true};
struct kml_counts { __u64 produced, sampled, lost, rejected; } counts;
static int paths, events;
static __u32 path_bytes;
static bool selected, pending, admission, ring_available;
static unsigned deletes, emitted;
'''
HELPERS = r'''
static struct pending_path saved;
static struct file_event event;
static __u64 bpf_get_current_pid_tgid(void) { return 42; }
static __u64 selected_time(void) { return selected ? 100 : 0; }
static bool regular_file(struct file *file) { return file && file->regular; }
static struct kml_counts *candidate(void) {
    if (!admission) return NULL;
    counts.produced++;
    return &counts;
}
static void *bpf_map_lookup_elem(void *map, const __u64 *key) {
    assert(map == &paths && *key == 42);
    return pending ? &saved : NULL;
}
static int bpf_map_delete_elem(void *map, const __u64 *key) {
    assert(map == &paths && *key == 42);
    deletes++;
    pending = false;
    return 0;
}
static void *bpf_ringbuf_reserve(void *map, unsigned size, unsigned flags) {
    assert(map == &events && size == sizeof(event) && !flags);
    return ring_available ? &event : NULL;
}
static void bpf_ringbuf_submit(void *value, unsigned flags) {
    assert(value == &event && !flags);
    emitted++;
}
'''
TEST = r'''
static void reset(void) {
    memset(&counts, 0, sizeof(counts));
    memset(&saved, 0, sizeof(saved));
    memset(&event, 0x5a, sizeof(event));
    selected = pending = admission = ring_available = target.regular = true;
    path_bytes = 64;
    deletes = emitted = 0;
    saved.file = (__u64)&target;
    saved.operation = 1;
    saved.length = 8;
    memcpy(saved.path, "/fixture", 8);
}
static void run(void) { assert(finish(&target, 4096, 128, 1) == 0); }
static void expect(unsigned delivered, unsigned removed, __u64 rejected, __u64 lost) {
    assert(emitted == delivered && deletes == removed);
    assert(counts.rejected == rejected && counts.lost == lost);
    if (removed) assert(!pending);
}
int main(void) {
    // Ordinary selected completion retains exact output and clears pending state.
    reset(); run(); expect(1, 1, 0, 0);
    assert(event.monotonic_ns == 100 && event.operation == 1);
    assert(event.requested == 4096 && event.completed == 128);
    assert(event.path_length == 8 && !memcmp(event.path, "/fixture", 8));
    for (unsigned i = 8; i < sizeof(event.path); i++) assert(event.path[i] == 0);
    // A second completion must not reuse the first operation's path.
    run(); expect(1, 1, 1, 0);
    assert(counts.produced == 2);

    // Non-selected callers neither emit nor delete an absent entry.
    reset(); selected = pending = false; run(); expect(0, 0, 0, 0);
    assert(counts.produced == 0);
    // Target departure, disabled collection or expiry can leave a pending entry.
    // selected_time returns zero for all three; the exit must still discard it.
    reset(); selected = false; run(); expect(0, 1, 0, 0);
    assert(counts.produced == 0);
    reset(); target.regular = false; run(); expect(0, 1, 0, 0);
    reset(); admission = false; run(); expect(0, 1, 0, 0);

    // Rejection and ring pressure preserve loss accounting and cleanup.
    reset(); saved.file = (__u64)&other; run(); expect(0, 1, 1, 0);
    reset(); saved.operation = 2; run(); expect(0, 1, 1, 0);
    reset(); saved.length = 0; run(); expect(0, 1, 1, 0);
    reset(); saved.length = 65; run(); expect(0, 1, 1, 0);
    reset(); pending = false; run(); expect(0, 0, 1, 0);
    reset(); path_bytes = 513; run(); expect(0, 1, 1, 0);
    reset(); ring_available = false; run(); expect(0, 1, 0, 1);
    reset(); assert(finish(&target, 4096, -1, 1) == 0); expect(0, 1, 1, 0);
    reset(); assert(finish(&target, 4096, 4097, 1) == 0); expect(0, 1, 1, 0);

    // Path-omitting sessions keep their existing no-map behaviour.
    reset(); path_bytes = 0; pending = false; run(); expect(1, 0, 0, 0);
    assert(event.path_length == 0);
    for (unsigned i = 0; i < sizeof(event.path); i++) assert(event.path[i] == 0);
    return 0;
}
'''


def declaration(source, marker, semicolon=False):
    start = source.index(marker)
    opening = source.index('{', start)
    depth, end = 1, opening + 1
    while depth:
        depth += (source[end] == '{') - (source[end] == '}')
        end += 1
    return source[start:end] + (';' if semicolon else '')


class PendingPathContract(unittest.TestCase):
    def test_exact_producer_exit_retains_cleanup_and_avoids_empty_deletion(self):
        source = PRODUCER.read_text()
        structs = '\n'.join(declaration(source, marker, True) for marker in (
            'struct file_event {', 'struct pending_path {'))
        copy = declaration(source, 'static __always_inline void copy_path_prefix(')
        finish = declaration(source, 'static __always_inline int finish(')
        with tempfile.TemporaryDirectory(prefix='kml-pending-path-') as temp:
            root = Path(temp)
            path = root / 'contract.c'
            path.write_text(PREFIX + structs + HELPERS + copy + finish + TEST)
            compiler = os.environ.get('CC', 'cc')
            built = subprocess.run([compiler, '-std=c11', '-Wall', '-Wextra', '-Werror',
                                    '-Wno-unknown-pragmas', '-O2', str(path), '-o', str(root / 'contract')],
                                   capture_output=True, timeout=30)
            self.assertEqual(built.returncode, 0, built.stderr.decode(errors='replace'))
            result = subprocess.run([str(root / 'contract')], capture_output=True, timeout=5)
            self.assertEqual(result.returncode, 0, result.stderr.decode(errors='replace'))


if __name__ == '__main__':
    unittest.main()
