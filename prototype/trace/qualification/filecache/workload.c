// SPDX-License-Identifier: Apache-2.0
// Local qualification only: fixed owned file, deterministic bytes, bounded I/O.
#define _GNU_SOURCE
#include <errno.h>
#include <fcntl.h>
#include <inttypes.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/mman.h>
#include <sys/stat.h>
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

int main(int argc, char **argv) {
    if (argc != 2) fail("mode");
    int prepare = strcmp(argv[1], "prepare") == 0;
    int cached = strcmp(argv[1], "cached") == 0;
    int uncached = strcmp(argv[1], "uncached") == 0;
    int writing = strcmp(argv[1], "write") == 0;
    int noise = strcmp(argv[1], "noise") == 0;
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
    unsigned int before = 0;
    uint64_t read_bytes = 0, write_bytes = 0;
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
    printf("{\"mode\":\"%s\",\"fileBytes\":%d,\"pageBytes\":%zu,"
           "\"residentPagesBefore\":%u,\"residentPagesAfter\":%u,"
           "\"readBytes\":%" PRIu64 ",\"writeBytes\":%" PRIu64 "}\n",
           argv[1], FILE_BYTES, page, before, after, read_bytes, write_bytes);
    return 0;
}
