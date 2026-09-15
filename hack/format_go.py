"""Format repository Go sources, preserving ignored dependency/source caches."""
import argparse
from pathlib import Path
import subprocess
import sys


def run(write=False):
    paths = subprocess.check_output([
        "git", "ls-files", "-z", "--cached", "--others", "--exclude-standard", "--", "*.go",
    ]).decode().split("\0")
    files = sorted({p for p in paths if p and Path(p).is_file()})
    if not files:
        return 0
    result = subprocess.run(["gofmt", "-w" if write else "-l", "--", *files],
                            capture_output=True, text=True)
    if result.stderr:
        sys.stderr.write(result.stderr)
    if result.stdout:
        sys.stdout.write(result.stdout)
    return result.returncode or int(bool(result.stdout))


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--write", action="store_true")
    sys.exit(run(parser.parse_args().write))
