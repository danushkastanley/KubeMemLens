// SPDX-License-Identifier: Apache-2.0
// Local qualification fixture only: bounded anonymous allocation, no policy writes.
#define _GNU_SOURCE
#include <stdint.h>
#include <stdio.h>
#include <string.h>
#include <sys/mman.h>
#include <time.h>
#include <unistd.h>

#define MIB (1024U * 1024U)
#define MAX_MIB 96U

int main(int argc, char **argv)
{
	if (argc != 2 || (strcmp(argv[1], "verify") && strcmp(argv[1], "consume"))) {
		fputs("invalid workload mode\n", stderr);
		return 2;
	}
	int verify = !strcmp(argv[1], "verify");
	unsigned int chunks = verify ? 2 : MAX_MIB;
	long page = sysconf(_SC_PAGESIZE);
	if (page != 4096 && page != 65536) return 2;
	void *allocations[MAX_MIB] = {};
	unsigned int allocated = 0;
	for (; allocated < chunks; allocated++) {
		void *memory = mmap(NULL, MIB, PROT_READ | PROT_WRITE,
			MAP_PRIVATE | MAP_ANONYMOUS, -1, 0);
		if (memory == MAP_FAILED) break;
		allocations[allocated] = memory;
		volatile unsigned char *bytes = memory;
		for (size_t offset = 0; offset < MIB; offset += (size_t)page)
			bytes[offset] = (unsigned char)(allocated ^ (offset / (size_t)page) ^ 0x5aU);
		for (size_t offset = 0; offset < MIB; offset += (size_t)page)
			if (bytes[offset] != (unsigned char)(allocated ^ (offset / (size_t)page) ^ 0x5aU))
				return 2;
	}
	int release_failed = 0;
	for (unsigned int i = 0; i < allocated; i++)
		if (munmap(allocations[i], MIB)) release_failed = 1;
	if (release_failed || allocated != chunks) {
		fputs("bounded allocation failed without confirmed OOM\n", stderr);
		return 2;
	}
	if (!verify) {
		fputs("allocation ceiling reached without expected OOM\n", stderr);
		return 3;
	}
	printf("{\"mode\":\"verify\",\"allocatedBytes\":%u,\"pageBytes\":%ld}\n", chunks * MIB, page);
	return 0;
}
