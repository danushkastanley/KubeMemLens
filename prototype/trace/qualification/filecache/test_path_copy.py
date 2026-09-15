"""Run the exact producer copy helper against d_path-style dirty suffixes; no BPF."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess


PRODUCER = Path(__file__).resolve().parents[2] / "programmes/filecache/files.bpf.c"
PREFIX = """
#include <stdint.h>
#include <string.h>
typedef uint32_t __u32;
#ifndef __always_inline
#define __always_inline inline __attribute__((always_inline))
#endif
"""
TEST = """
int main(void) {
    const unsigned lengths[] = {0, 1, 19, 63, 255, 511, 512};
    for (unsigned n = 0; n < sizeof(lengths)/sizeof(lengths[0]); n++) {
        unsigned length = lengths[n], offset = 512 - length;
        char source[513] = {0}, guarded[515] = {0};
        guarded[0] = guarded[514] = 42;
        // Match the kernel helper: construct backwards, then memmove only the
        // returned string and NUL. Non-prefix scratch bytes remain untouched.
        for (unsigned i = 0; i < length; i++)
            source[offset+i] = 'a' + (char)(i % 26);
        memmove(source, source + offset, length + 1);
        copy_path_prefix(guarded + 1, source, length);
        if (memcmp(guarded + 1, source, length) != 0) return 11;
        for (unsigned i = length; i < 513; i++)
            if (guarded[i+1] != 0) return 12;
        if (guarded[0] != 42 || guarded[514] != 42) return 13;
    }
    return 0;
}
"""


def run(compiler, output):
    if not re.fullmatch(r"sha256:[0-9a-f]{64}", compiler):
        raise ValueError("immutable local compiler image ID required")
    source = PRODUCER.read_text()
    start = source.index("static __always_inline void copy_path_prefix(")
    opening = source.index("{", start)
    depth, end = 1, opening + 1
    while depth:
        depth += (source[end] == "{") - (source[end] == "}")
        end += 1
    helper = source[start:end]
    output = output.resolve()
    output.mkdir(mode=0o700)
    (output / "fixed.c").write_text(PREFIX + helper + TEST)
    old = """static void copy_path_prefix(char *destination, const char *source, __u32 length) {
        (void)length;
        memcpy(destination, source, 513);
    }
    """
    (output / "prior.c").write_text(PREFIX + old + TEST)
    command = ["docker", "run", "--rm", "--network=none", "--cap-drop=ALL",
               "--security-opt=no-new-privileges", "--read-only", "--cpus=2",
               "--memory=512m", "--pids-limit=64", "--user", f"{os.getuid()}:{os.getgid()}",
               "--tmpfs", "/tmp:rw,nosuid,noexec,size=64m",
               "--mount", f"type=bind,source={output},target=/test",
               compiler, "sh", "-ec", """
for name in fixed prior; do
  gcc -std=c11 -Wall -Wextra -Werror -Wno-unknown-pragmas -O2 -static \
    -o /test/$name /test/$name.c
done
/test/fixed
set +e
/test/prior
status=$?
set -e
test "$status" -eq 12
"""]
    subprocess.run(command, check=True, timeout=30)
    sha = lambda data: hashlib.sha256(data).hexdigest()
    record = {"producerSHA256": sha(source.encode()), "helperSHA256": sha(helper.encode()),
              "compilerImage": compiler, "fixedHelper": "passed", "priorFullCopy": "rejected dirty suffix",
              "lengths": [0, 1, 19, 63, 255, 511, 512], "guardBytesPreserved": True,
              "kernelProgrammeLoaded": False}
    (output / "result.json").write_text(json.dumps(record, indent=2) + "\n")
    print("Exact producer helper passed; prior full-buffer copy exposes helper scratch bytes.")


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--compiler-image", required=True)
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    run(args.compiler_image, args.output)
