// SPDX-License-Identifier: Apache-2.0
// Local qualification only: fixed owned file, deterministic bytes, bounded I/O.
#define _GNU_SOURCE
#include <errno.h>
#include <fcntl.h>
#include <inttypes.h>
#include <poll.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/mman.h>
#include <sys/stat.h>
#include <time.h>
#include <unistd.h>

#define BLOCK_BYTES 65536
#define FILE_BYTES (8 * 1024 * 1024)
static const char *fixture = "/work/fixed-seed.bin";
static unsigned char expected[BLOCK_BYTES], actual[BLOCK_BYTES];

static void fail(const char *stage) {
    // Fixed categories only: no file bytes, paths or runtime identifiers.
    fprintf(stderr, "workload failed: %s\n", stage);
    exit(2);
}

static uint64_t clock_nanos(clockid_t clock) {
    struct timespec value;
    if (clock_gettime(clock, &value) != 0 || value.tv_sec < 0 ||
        (uint64_t)value.tv_sec > (UINT64_MAX - 999999999) / 1000000000 ||
        value.tv_nsec < 0 || value.tv_nsec >= 1000000000)
        fail("operation clock");
    return (uint64_t)value.tv_sec * UINT64_C(1000000000) + (uint64_t)value.tv_nsec;
}

static uint64_t monotonic_nanos(void) { return clock_nanos(CLOCK_MONOTONIC); }

static void seed_block(void) {
    uint32_t state = UINT32_C(0x5eed1234);
    for (size_t i = 0; i < sizeof(expected); i++) {
        state ^= state << 13;
        state ^= state >> 17;
        state ^= state << 5;
        expected[i] = (unsigned char)state;
    }
}

static void transfer(int fd, int writing) {
    if (lseek(fd, 0, SEEK_SET) != 0) fail("seek");
    for (size_t offset = 0; offset < FILE_BYTES; offset += BLOCK_BYTES) {
        size_t done = 0;
        while (done < BLOCK_BYTES) {
            ssize_t count = writing
                ? write(fd, expected + done, BLOCK_BYTES - done)
                : read(fd, actual + done, BLOCK_BYTES - done);
            if (count < 0 && errno == EINTR) continue;
            if (count <= 0) fail("transfer");
            done += (size_t)count;
        }
        if (!writing && memcmp(expected, actual, BLOCK_BYTES) != 0)
            fail("data integrity");
    }
}

static unsigned int resident(int fd, size_t page) {
    unsigned char vector[FILE_BYTES / 4096];
    size_t pages = FILE_BYTES / page;
    void *mapping = mmap(NULL, FILE_BYTES, PROT_NONE, MAP_SHARED, fd, 0);
    if (mapping == MAP_FAILED) fail("residency mapping");
    if (mincore(mapping, FILE_BYTES, vector) != 0) fail("residency query");
    unsigned int count = 0;
    for (size_t i = 0; i < pages; i++) count += vector[i] & 1;
    if (munmap(mapping, FILE_BYTES) != 0) fail("residency unmap");
    return count;
}

enum operation_start { START_IMMEDIATELY, WAIT_FOR_COMMAND };

static void wait_command(char command) {
    struct pollfd input = {STDIN_FILENO, POLLIN, 0};
    char value;
    if (poll(&input, 1, 30000) != 1 || !(input.revents & POLLIN)
        || read(STDIN_FILENO, &value, 1) != 1 || value != command)
        fail("bounded command");
}

struct observation {
    size_t page;
    unsigned int before, after;
    uint64_t read_bytes, write_bytes, started, ended;
};

static struct observation run_one(const char *mode, enum operation_start start) {
    int prepare = strcmp(mode, "prepare") == 0;
    int cached = strcmp(mode, "cached") == 0;
    int uncached = strcmp(mode, "uncached") == 0;
    int writing = strcmp(mode, "write") == 0;
    int noise = strcmp(mode, "noise") == 0;
    if (!prepare && !cached && !uncached && !writing && !noise) fail("mode");
    long native_page = sysconf(_SC_PAGESIZE);
    if (native_page != 4096 && native_page != 65536) fail("page size");
    size_t page = (size_t)native_page;
    seed_block();
    int flags = O_RDWR | O_CLOEXEC | O_NOFOLLOW;
    if (prepare) flags |= O_CREAT | O_EXCL;
    int fd = open(fixture, flags, 0600);
    if (fd < 0) fail("open owned fixture");
    struct stat info;
    if (fstat(fd, &info) != 0 || !S_ISREG(info.st_mode) || info.st_uid != getuid()
        || info.st_nlink != 1 || info.st_size != (prepare ? 0 : FILE_BYTES))
        fail("fixture identity");
    // Perform container startup and file opening before the trace attaches.
    if (start == WAIT_FOR_COMMAND) {
        if (puts("{\"ready\":true}") < 0 || fflush(stdout) != 0) fail("ready output");
        wait_command('R');
    }
    unsigned int before = 0;
    uint64_t read_bytes = 0, write_bytes = 0;
    uint64_t started = monotonic_nanos();
    if (uncached) {
        if (fdatasync(fd) != 0 || posix_fadvise(fd, 0, FILE_BYTES, POSIX_FADV_DONTNEED) != 0)
            fail("discard owned cache");
    }
    if (!prepare) before = resident(fd, page);
    if (cached && before != FILE_BYTES / page) fail("cache not fully resident");
    if (uncached && before != 0) fail("cache discard ineffective");
    if (prepare || writing || noise) {
        unsigned int rounds = noise ? 4 : 1;
        for (unsigned int i = 0; i < rounds; i++) {
            transfer(fd, 1);
            write_bytes += FILE_BYTES;
            if (noise) {
                transfer(fd, 0);
                read_bytes += FILE_BYTES;
            }
        }
        if (fdatasync(fd) != 0) fail("sync owned fixture");
    } else {
        transfer(fd, 0);
        read_bytes = FILE_BYTES;
    }
    unsigned int after = resident(fd, page);
    if (after != FILE_BYTES / page) fail("cache residency after I/O");
    if (close(fd) != 0) fail("close");
    uint64_t ended = monotonic_nanos();
    if (ended <= started) fail("operation clock");
    return (struct observation){page, before, after, read_bytes, write_bytes, started, ended};
}

static void emit_fields(const char *mode, struct observation value) {
    printf("{\"mode\":\"%s\",\"fileBytes\":%d,\"pageBytes\":%zu,"
           "\"residentPagesBefore\":%u,\"residentPagesAfter\":%u,"
           "\"readBytes\":%" PRIu64 ",\"writeBytes\":%" PRIu64,
           mode, FILE_BYTES, value.page, value.before, value.after, value.read_bytes, value.write_bytes);
}

static void emit_timed(const char *mode, struct observation value) {
    emit_fields(mode, value);
    printf(",\"operationStartedMonotonicNanos\":%" PRIu64 ","
           "\"operationEndedMonotonicNanos\":%" PRIu64 ","
           "\"operationNanos\":%" PRIu64 "}",
           value.started, value.ended, value.ended - value.started);
}

#define MAX_MIXED_SERIES 18000
static struct observation observations[MAX_MIXED_SERIES];

struct series_pattern {
    const char *record_type;
    unsigned int version, maximum_count, mode_count;
    const char *modes[3];
};

static const struct series_pattern mixed_pattern = {
    "mixed-series-start", 2, MAX_MIXED_SERIES, 3, {"cached", "uncached", "write"}
};

static unsigned int number(const char *text, unsigned int maximum) {
    unsigned int value = 0;
    if (*text == 0) fail("series arguments");
    for (const char *p = text; *p; p++) {
        if (*p < '0' || *p > '9' || value > (maximum - (unsigned int)(*p - '0')) / 10)
            fail("series arguments");
        value = value * 10 + (unsigned int)(*p - '0');
    }
    if (value == 0 || value > maximum) fail("series arguments");
    return value;
}

static void wait_until(uint64_t due) {
    struct timespec target = {(time_t)(due / UINT64_C(1000000000)), (long)(due % UINT64_C(1000000000))};
    int result;
    do { result = clock_nanosleep(CLOCK_MONOTONIC, TIMER_ABSTIME, &target, NULL); }
    while (result == EINTR);
    if (result != 0) fail("series scheduling");
}

static void emit_series(const struct series_pattern *pattern, unsigned int count, uint64_t first, uint64_t period) {
    for (unsigned int i = 0; i < count; i++) {
        printf("{\"sequence\":%u,\"dueMonotonicNanos\":%" PRIu64 ",\"observation\":", i, first + (uint64_t)i * period);
        emit_timed(pattern->modes[i % pattern->mode_count], observations[i]);
        puts("}");
    }
    if (fflush(stdout) != 0) fail("series output");
}

static int series(const struct series_pattern *pattern, const char *count_text, const char *period_text) {
    unsigned int count = number(count_text, pattern->maximum_count);
    unsigned int period_ms = number(period_text, 10000);
    if (count % pattern->mode_count || period_ms < 100 || (uint64_t)count * period_ms > UINT64_C(1800000))
        fail("series schedule bound");
    uint64_t period = (uint64_t)period_ms * UINT64_C(1000000);
    uint64_t before = monotonic_nanos(), wall = clock_nanos(CLOCK_REALTIME), after = monotonic_nanos();
    if (after < before || after - before > UINT64_C(1000000)) fail("series clock alignment");
    uint64_t first = after + UINT64_C(5000000000);
    alarm(16 + (count * period_ms + 999) / 1000);
    printf("{\"type\":\"%s\",\"schemaVersion\":%u,\"count\":%u,\"periodNanos\":%" PRIu64
           ",\"monotonicBeforeNanos\":%" PRIu64 ",\"wallNanos\":%" PRIu64
           ",\"monotonicAfterNanos\":%" PRIu64 ",\"firstDueNanos\":%" PRIu64 "}\n",
           pattern->record_type, pattern->version, count, period, before, wall, after, first);
    if (fflush(stdout) != 0) fail("series output");
    for (unsigned int i = 0; i < count; i++) {
        uint64_t due = first + (uint64_t)i * period;
        uint64_t cpu_before = clock_nanos(CLOCK_PROCESS_CPUTIME_ID);
        uint64_t wait_started = monotonic_nanos();
        wait_until(due);
        uint64_t woke = monotonic_nanos();
        uint64_t cpu_woke = clock_nanos(CLOCK_PROCESS_CPUTIME_ID);
        observations[i] = run_one(pattern->modes[i % pattern->mode_count], START_IMMEDIATELY);
        uint64_t cpu_ended = clock_nanos(CLOCK_PROCESS_CPUTIME_ID);
        if (observations[i].started < due || observations[i].ended >= due + period) {
            // Retain completed operations and the late one before stopping. The
            // incomplete/late stream still fails verification; no slot is retried.
            emit_series(pattern, i + 1, first, period);
            printf("{\"type\":\"series-deadline-failure\",\"schemaVersion\":1,\"sequence\":%u,"
                   "\"waitStartedMonotonicNanos\":%" PRIu64 ",\"wokeMonotonicNanos\":%" PRIu64 ","
                   "\"cpuBeforeWaitNanos\":%" PRIu64 ",\"cpuAfterWakeNanos\":%" PRIu64 ","
                   "\"cpuAfterOperationNanos\":%" PRIu64 "}\n",
                   i, wait_started, woke, cpu_before, cpu_woke, cpu_ended);
            if (fflush(stdout) != 0) fail("series output");
            fail("series deadline missed");
        }
    }
    // Avoid turning measurement reports into traced I/O during the workload.
    wait_until(first + (uint64_t)count * period);
    emit_series(pattern, count, first, period);
    return 0;
}

int main(int argc, char **argv) {
    if (argc >= 2 && strcmp(argv[1], "series") == 0) {
        if (argc != 5) fail("series arguments");
        const char *mode = argv[2];
        if (strcmp(mode, "cached") && strcmp(mode, "uncached") && strcmp(mode, "write") && strcmp(mode, "noise"))
            fail("series mode");
        const struct series_pattern uniform = {"series-start", 1, 1800, 1, {mode}};
        return series(&uniform, argv[3], argv[4]);
    }
    if (argc >= 2 && strcmp(argv[1], "mixed-series") == 0) {
        if (argc != 4) fail("series arguments");
        return series(&mixed_pattern, argv[2], argv[3]);
    }
    int gated = argc == 3 && strcmp(argv[2], "--gated") == 0;
    if (argc != 2 && !gated) fail("mode");
    if (gated && strcmp(argv[1], "cached") != 0 && strcmp(argv[1], "uncached") != 0)
        fail("mode");
    if (strcmp(argv[1], "idle") == 0) { sleep(1800); return 0; }
    if (strcmp(argv[1], "paired-idle") == 0) { sleep(3600); return 0; }
    alarm(gated ? 70 : 10);
    struct observation value = run_one(argv[1], gated ? WAIT_FOR_COMMAND : START_IMMEDIATELY);
    // Preserve the exact seven-field receipt consumed by isolation qualification.
    emit_fields(argv[1], value);
    puts("}");
    if (gated) {
        if (fflush(stdout) != 0) fail("receipt output");
        wait_command('Q');
    }
    return 0;
}
