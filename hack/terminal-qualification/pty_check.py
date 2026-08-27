#!/usr/bin/env python3
"""Exercise the real TUI in a pseudo-terminal and retain sanitised evidence."""

from __future__ import annotations

import argparse
import fcntl
import json
import os
import pty
import re
import select
import signal
import struct
import subprocess
import sys
import termios
import time
from pathlib import Path

ALT_ENTER = b"\x1b[?1049h"
ALT_EXIT = b"\x1b[?1049l"
CURSOR_HIDE = b"\x1b[?25l"
CURSOR_SHOW = b"\x1b[?25h"
TITLE_PATTERN = re.compile(br"\x1b\](?:0|2);")
SGR_PATTERN = re.compile(br"\x1b\[([0-9;]*)m")
SAFE_NAME = re.compile(r"^[a-z0-9][a-z0-9._-]{0,63}$")
MAX_CAPTURE_BYTES = 32 * 1024 * 1024
MAX_AVERAGE_BYTES_PER_SECOND = 512 * 1024


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser()
    parser.add_argument("--cli", required=True)
    parser.add_argument("--kubeconfig", required=True)
    parser.add_argument("--context", required=True)
    parser.add_argument("--collector-namespace", default="kube-memlens")
    parser.add_argument("--profile", required=True)
    parser.add_argument("--terminal-version", required=True)
    parser.add_argument("--term", required=True)
    parser.add_argument("--columns", type=int, required=True)
    parser.add_argument("--rows", type=int, required=True)
    parser.add_argument("--duration-seconds", type=int, default=5)
    parser.add_argument("--refresh", default="1s")
    parser.add_argument("--exit-mode", choices=("normal", "ctrl-c", "sigint", "sigterm"), default="normal")
    parser.add_argument("--colour-mode", choices=("no-color", "low-color", "native"), default="native")
    parser.add_argument("--transport", choices=("direct", "tmux", "ssh"), default="direct")
    parser.add_argument("--expected-screen", choices=("dashboard", "connection-error"), default="dashboard")
    parser.add_argument("--ssh-port", type=int, default=22)
    parser.add_argument("--ssh-user", default="qualification")
    parser.add_argument("--ssh-identity")
    parser.add_argument("--ssh-known-hosts")
    parser.add_argument("--output", required=True)
    return parser.parse_args()


def validate_args(args: argparse.Namespace) -> None:
    for label, value in (("profile", args.profile), ("terminal version", args.terminal_version), ("TERM", args.term)):
        if not SAFE_NAME.fullmatch(value):
            raise ValueError(f"{label} must match {SAFE_NAME.pattern}")
    if not Path(args.cli).is_file() or not os.access(args.cli, os.X_OK):
        raise ValueError("--cli must be an executable file")
    if not Path(args.kubeconfig).is_file():
        raise ValueError("--kubeconfig must be a file")
    if args.columns < 1 or args.columns > 500 or args.rows < 1 or args.rows > 200:
        raise ValueError("terminal dimensions are outside the 1..500 by 1..200 bound")
    if args.duration_seconds < 1 or args.duration_seconds > 3600:
        raise ValueError("--duration-seconds must be between 1 and 3600")
    if args.transport == "ssh":
        if not 1 <= args.ssh_port <= 65535 or not SAFE_NAME.fullmatch(args.ssh_user):
            raise ValueError("SSH port or user is invalid")
        for label, value in (("identity", args.ssh_identity), ("known-hosts", args.ssh_known_hosts)):
            if not value or not Path(value).is_file():
                raise ValueError(f"SSH {label} must be a file")
    output = Path(args.output)
    if output.exists():
        raise ValueError("refusing to overwrite --output")
    if not output.parent.is_dir():
        raise ValueError("--output parent directory must exist")


def set_window_size(fd: int, columns: int, rows: int) -> None:
    fcntl.ioctl(fd, termios.TIOCSWINSZ, struct.pack("HHHH", rows, columns, 0, 0))


def colour_sgr_count(data: bytes) -> int:
    count = 0
    for match in SGR_PATTERN.finditer(data):
        params = [int(value) for value in match.group(1).split(b";") if value]
        if any(30 <= value <= 49 or 90 <= value <= 107 for value in params):
            count += 1
    return count


def terminal_modes_equal(before: list[object], after: list[object]) -> bool:
    """Compare terminal state while ignoring the transient pending-input flag."""
    normalised_before = list(before)
    normalised_after = list(after)
    pending_input = getattr(termios, "PENDIN", 0)
    normalised_before[3] = int(normalised_before[3]) & ~pending_input
    normalised_after[3] = int(normalised_after[3]) & ~pending_input
    return normalised_before == normalised_after


class Capture:
    def __init__(self) -> None:
        self.data = bytearray()
        self.total = 0
        self.truncated = False

    def add(self, chunk: bytes) -> None:
        self.total += len(chunk)
        remaining = MAX_CAPTURE_BYTES - len(self.data)
        if remaining > 0:
            self.data.extend(chunk[:remaining])
        if len(chunk) > remaining:
            self.truncated = True

    def contains(self, token: bytes) -> bool:
        return token in self.data


def read_available(master: int, capture: Capture, timeout: float) -> bool:
    readable, _, _ = select.select([master], [], [], timeout)
    if not readable:
        return False
    try:
        chunk = os.read(master, 65536)
    except OSError:
        return False
    if not chunk:
        return False
    capture.add(chunk)
    return True


def wait_for(master: int, capture: Capture, token: bytes, deadline: float) -> None:
    while time.monotonic() < deadline:
        if capture.contains(token):
            return
        read_available(master, capture, 0.1)
    raise RuntimeError(f"timed out waiting for {token.decode('utf-8', 'replace')}")


def exercise(master: int, capture: Capture, columns: int, rows: int, finish_at: float, expected_screen: str) -> None:
    wait_for(master, capture, b"KubeMemLens", min(finish_at, time.monotonic() + 30))
    screen_token = b"A/F/S/O" if expected_screen == "dashboard" else b"connection error"
    wait_for(master, capture, screen_token, min(finish_at, time.monotonic() + 30))
    actions = (b"G", b"s", b"N", b"p", b"?", b"?", b" ", b" ")
    action_index = 0
    resize_index = 0
    sizes = ((columns, rows), (max(1, columns - 17), max(1, rows - 7)), (39, 9), (40, 10), (columns, rows))
    next_action = time.monotonic()
    while time.monotonic() < finish_at:
        read_available(master, capture, 0.1)
        now = time.monotonic()
        if now < next_action:
            continue
        os.write(master, actions[action_index % len(actions)])
        action_index += 1
        size = sizes[resize_index % len(sizes)]
        set_window_size(master, size[0], size[1])
        resize_index += 1
        next_action = now + 1
    set_window_size(master, columns, rows)


def terminate(process: subprocess.Popen[bytes], master: int, mode: str) -> None:
    if mode == "normal":
        os.write(master, b"q")
    elif mode == "ctrl-c":
        os.write(master, b"\x03")
    elif mode == "sigint":
        os.killpg(process.pid, signal.SIGINT)
    else:
        os.killpg(process.pid, signal.SIGTERM)


def run(args: argparse.Namespace) -> dict[str, object]:
    master, slave = pty.openpty()
    set_window_size(slave, args.columns, args.rows)
    before = termios.tcgetattr(slave)
    environment = os.environ.copy()
    environment["TERM"] = args.term
    if args.colour_mode == "no-color":
        environment["NO_COLOR"] = "1"
    else:
        environment.pop("NO_COLOR", None)
    application = [
        args.cli,
        "--kubeconfig", args.kubeconfig,
        "--context", args.context,
        "--collector-namespace", args.collector_namespace,
        "tui", "--all-namespaces", "--refresh", args.refresh,
    ]
    command = transport_command(args, application)
    started = time.monotonic()
    process = subprocess.Popen(command, stdin=slave, stdout=slave, stderr=slave, env=environment, start_new_session=True)
    capture = Capture()
    try:
        finish_at = started + args.duration_seconds
        exercise(master, capture, args.columns, args.rows, finish_at, args.expected_screen)
        terminate(process, master, args.exit_mode)
        exit_code = process.wait(timeout=15)
        while read_available(master, capture, 0.05):
            pass
        after = termios.tcgetattr(slave)
    finally:
        if process.poll() is None:
            os.killpg(process.pid, signal.SIGKILL)
            process.wait(timeout=5)
        os.close(master)
        os.close(slave)
    elapsed = max(time.monotonic() - started, 0.001)
    data = bytes(capture.data)
    checks = {
        "applicationRendered": b"KubeMemLens" in data and (
            (args.expected_screen == "dashboard" and b"A/F/S/O" in data)
            or (args.expected_screen == "connection-error" and b"connection error" in data)
        ),
        "alternateScreenRestored": data.count(ALT_ENTER) > 0 and data.count(ALT_ENTER) == data.count(ALT_EXIT),
        "cursorRestored": data.count(CURSOR_HIDE) > 0 and data.count(CURSOR_SHOW) >= data.count(CURSOR_HIDE),
        "terminalModeRestored": terminal_modes_equal(before, after),
        "titleUnchanged": TITLE_PATTERN.search(data) is None,
        "utf8Valid": decode_errors(data) == 0,
        "captureComplete": not capture.truncated,
        "outputBounded": capture.total / elapsed <= MAX_AVERAGE_BYTES_PER_SECOND,
        "noColourCodes": args.colour_mode != "no-color" or colour_sgr_count(data) == 0,
    }
    outcome = "passed" if all(checks.values()) else "failed"
    return {
        "schemaVersion": 1,
        "outcome": outcome,
        "terminal": {
            "profile": args.profile,
            "version": args.terminal_version,
            "term": args.term,
            "colourMode": args.colour_mode,
            "columns": args.columns,
            "rows": args.rows,
        },
        "run": {
            "exitMode": args.exit_mode,
            "transport": args.transport,
            "expectedScreen": args.expected_screen,
            "refresh": args.refresh,
            "requestedDurationSeconds": args.duration_seconds,
            "elapsedSeconds": round(elapsed, 3),
            "exitCode": exit_code,
        },
        "checks": checks,
        "metrics": {
            "outputBytes": capture.total,
            "averageBytesPerSecond": round(capture.total / elapsed, 3),
            "colourSGRSequences": colour_sgr_count(data),
            "titleSequences": len(TITLE_PATTERN.findall(data)),
        },
        "privacy": {"rawTerminalOutputRetained": False, "credentialPathsRetained": False, "clusterIdentifiersRetained": False},
    }


def transport_command(args: argparse.Namespace, application: list[str]) -> list[str]:
    if args.transport == "direct":
        return application
    if args.transport == "tmux":
        return [
            "tmux", "-L", "kube-memlens-prod009", "new-session",
            "-x", str(args.columns), "-y", str(args.rows), *application,
        ]
    return [
        "ssh", "-tt", "-p", str(args.ssh_port),
        "-i", args.ssh_identity,
        "-o", "BatchMode=yes",
        "-o", "IdentitiesOnly=yes",
        "-o", "StrictHostKeyChecking=yes",
        "-o", f"UserKnownHostsFile={args.ssh_known_hosts}",
        f"{args.ssh_user}@127.0.0.1", *application,
    ]


def decode_errors(data: bytes) -> int:
    return data.decode("utf-8", "replace").count("\ufffd")


def write_result(path: Path, result: dict[str, object]) -> None:
    descriptor = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(descriptor, "w", encoding="utf-8") as handle:
        json.dump(result, handle, indent=2, sort_keys=True)
        handle.write("\n")


def main() -> int:
    args = parse_args()
    try:
        validate_args(args)
        result = run(args)
        write_result(Path(args.output), result)
    except (OSError, ValueError, RuntimeError, subprocess.SubprocessError) as error:
        print(f"terminal PTY check failed: {error}", file=sys.stderr)
        return 1
    print(f"terminal PTY check {result['outcome']}: {args.profile} {args.columns}x{args.rows} {args.exit_mode}")
    return 0 if result["outcome"] == "passed" else 1


if __name__ == "__main__":
    raise SystemExit(main())
