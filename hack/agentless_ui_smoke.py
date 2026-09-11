#!/usr/bin/env python3
"""Exercise restricted CLI and PTY workflows; retain only sanitised assertions."""

import argparse
import importlib.util
import json
import os
import pty
import signal
import subprocess
import sys
import termios
import time
from pathlib import Path

spec = importlib.util.spec_from_file_location(
    "terminal_pty", Path(__file__).parent / "terminal-qualification" / "pty_check.py"
)
terminal = importlib.util.module_from_spec(spec)
spec.loader.exec_module(terminal)


def cli_json(args, command):
    result = subprocess.run(
        [args.cli, "--kubeconfig", args.kubeconfig, "--mode=restricted", *command,
         "-n", args.namespace, "-o", "json"],
        capture_output=True, check=True, timeout=20,
    )
    for forbidden in (b'"totalBytes"', b'"recentEvents"', b'"cgroupPath"', b'"labels"', b'"podUID"', b"Bearer "):
        if forbidden in result.stdout:
            raise RuntimeError("restricted machine output contains unavailable deep fields or private metadata")
    return json.loads(result.stdout)


def check_cli(args):
    for entity in ("pods", "containers", "workloads", "ns"):
        rows = cli_json(args, ["top", entity])
        if not rows or any(row["mode"] != "restricted" for row in rows):
            raise RuntimeError("restricted top did not return source-labelled rows")
    containers = cli_json(args, ["top", "containers"])
    values = {row["name"]: row["workingSet"]["bytes"] for row in containers}
    if values != {"worker": 32 << 20, "missing": None}:
        raise RuntimeError("working set and missing memory were not distinct")
    for operation, version in (("explain", 4), ("recommend", 2)):
        report = cli_json(args, [operation, "pod", "fixture-pod"])
        if report["schemaVersion"] != version or report["automaticMutation"]:
            raise RuntimeError("restricted report contract or read-only action changed")
    report = cli_json(args, ["explain", "workload", "Pod/fixture-pod"])
    if len(report["children"]) != 1:
        raise RuntimeError("workload drill did not retain its Pod")


def change_access(args, grant):
    command = ["kubectl", "--kubeconfig", args.admin_kubeconfig, "--context", args.admin_context,
               "-n", args.namespace]
    if grant:
        command += ["create", "rolebinding", "restricted-reader", "--role=restricted-reader",
                    "--serviceaccount=" + args.namespace + ":restricted-reader"]
    else:
        command += ["delete", "rolebinding", "restricted-reader"]
    result = subprocess.run(command, capture_output=True, timeout=20)
    if result.returncode:
        raise RuntimeError("could not update the disposable reader permission")


def action(process, master, capture, key, token, columns, rows):
    start = len(capture.data)
    os.write(master, key)
    # Observe the minimum-size frame before restoring dimensions. This makes
    # redraw acknowledgement explicit, without sleeps or stale-token matches.
    terminal.set_window_size(master, 39, 9)
    os.killpg(process.pid, signal.SIGWINCH)
    wait_new(process, master, capture, start, b"Terminal too small")
    start = len(capture.data)
    terminal.set_window_size(master, columns, rows)
    os.killpg(process.pid, signal.SIGWINCH)
    wait_new(process, master, capture, start, token)


def wait_new(process, master, capture, start, token):
    deadline = time.monotonic() + 15
    while time.monotonic() < deadline:
        if token in capture.data[start:]:
            return
        if process.poll() is not None:
            raise RuntimeError("restricted TUI exited during an interaction")
        terminal.read_available(master, capture, 0.1)
    raise RuntimeError("restricted PTY did not reach the expected interaction state: " + token.decode())


def check_pty(args, columns, rows, full):
    master, slave = pty.openpty()
    terminal.set_window_size(slave, columns, rows)
    before = termios.tcgetattr(slave)
    environment = {**os.environ, "TERM": "xterm-256color", "NO_COLOR": "1"}
    command = [args.cli, "--kubeconfig", args.kubeconfig, "--mode=restricted",
               "tui", "-n", args.namespace, "--refresh=1s"]
    process = subprocess.Popen(command, stdin=slave, stdout=slave, stderr=slave,
                               env=environment, start_new_session=True)
    capture = terminal.Capture()
    revoked = False
    try:
        terminal.wait_for(master, capture, b"restricted / Kubernetes APIs", time.monotonic() + 20)
        terminal.wait_for(master, capture, b"fixture-pod", time.monotonic() + 20)
        terminal.wait_for(master, capture, b"WORKING SET", time.monotonic() + 10)
        send = lambda key, token: action(process, master, capture, key, token, columns, rows)
        send(b"n", b"/ namespaces")
        send(b"\r", b"fixture-pod")
        send(b"w", b"Pod/fixture-pod")
        send(b"\r", b"fixture-pod")
        send(b"c", b"worker")
        send(b"p", b"fixture-pod")
        send(b"N", b"No authorised observations")
        send(b"p", b"fixture-pod")
        send(b"e", b"Working set:")
        send(b"h", b"WORKING SET")
        if full:
            send(b"s", b"name asc")
            send(b"/", b"search:")
            os.write(master, b"fixture-pod\r")
            send(b" ", b"paused")
            send(b"r", b"paused")
            send(b" ", b"automatic")
            send(b"?", b"cluster policy")
            send(b"?", b"WORKING SET")
            send(b"R", b"Automatic mutation: disabled")
            send(b"\r", b"WORKING SET")
            send(b"C", b"restricted capture is unavailable")
            send(b"\r", b"WORKING SET")
            send(b"x", b"restricted comparison is unavailable")
            send(b"\r", b"WORKING SET")
            change_access(args, False)
            revoked = True
            send(b"r", b"access revoked")
            change_access(args, True)
            revoked = False
            send(b"r", b"fixture-pod")
        os.write(master, b"q")
        # Keep draining the PTY while shutdown restores terminal modes. Waiting
        # without reading can block the child on its final display writes.
        finish = time.monotonic() + 10
        while process.poll() is None and time.monotonic() < finish:
            terminal.read_available(master, capture, 0.1)
        if process.poll() is None:
            raise RuntimeError("restricted TUI did not exit after quit")
        exit_code = process.wait(timeout=1)
        while terminal.read_available(master, capture, 0.05):
            pass
        after = termios.tcgetattr(slave)
        if exit_code or capture.truncated or not terminal.terminal_modes_equal(before, after):
            raise RuntimeError("restricted TUI exit or terminal-state restoration failed")
        data = bytes(capture.data)
        if terminal.ALT_ENTER not in data or terminal.ALT_EXIT not in data or terminal.CURSOR_SHOW not in data:
            raise RuntimeError("restricted TUI did not restore terminal display state")
        if terminal.colour_sgr_count(data) or not terminal.renderer_isolated(data):
            raise RuntimeError("restricted NO_COLOR rendering was mixed with colours or client logs")
        return {"size": f"{columns}x{rows}", "outcome": "passed", "capturedBytes": capture.total,
                "terminalRestored": True, "rawPTYRetained": False}
    finally:
        if process.poll() is None:
            os.killpg(process.pid, signal.SIGKILL)
            process.wait(timeout=10)
        os.close(master)
        os.close(slave)
        if revoked:
            change_access(args, True)


def main():
    parser = argparse.ArgumentParser()
    for name in ("cli", "kubeconfig", "namespace", "admin-kubeconfig", "admin-context", "output"):
        parser.add_argument("--" + name, required=True)
    args = parser.parse_args()
    if not args.admin_context.startswith("kind-") or args.namespace != "kube-memlens-agentless-a":
        raise ValueError("UI smoke requires the disposable agentless kind fixture")
    owner = subprocess.run(["kubectl", "--kubeconfig", args.admin_kubeconfig, "--context", args.admin_context,
                            "get", "namespace", args.namespace, "-o", "json"],
                           capture_output=True, check=True, timeout=20)
    if json.loads(owner.stdout)["metadata"].get("labels", {}).get("app.kubernetes.io/managed-by") != "kube-memlens-agentless-e2e":
        raise ValueError("refusing to alter a namespace not owned by the disposable fixture")
    check_cli(args)
    terminals = [check_pty(args, width, height, index == 0)
                 for index, (width, height) in enumerate(((80, 24), (120, 30), (180, 50)))]
    terminal.write_result(Path(args.output), {"outcome": "passed", "cli": "passed", "terminals": terminals,
        "checks": ["working-set source", "missing metrics", "navigation", "filter", "sort", "pause", "refresh",
                   "detail", "read-only recommendations", "unsupported actions", "permission revocation and recovery"],
        "credentialsRetained": False, "runtimeIdentifiersIncluded": False})
    print("restricted CLI and PTY smoke passed")


if __name__ == "__main__":
    try:
        main()
    except (OSError, ValueError, RuntimeError, subprocess.SubprocessError) as error:
        print(f"restricted UI smoke failed: {error}", file=sys.stderr)
        raise SystemExit(1)
