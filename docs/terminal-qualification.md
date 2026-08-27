# Terminal qualification

Terminal support claims require a real emulator run. Setting `TERM` in a
pseudo-terminal tests capability handling, but it does not test an emulator's
renderer, fonts or window lifecycle.

The PROD-009 qualification has three layers:

1. Go tests cover layout bounds, navigation, selection recovery, `NO_COLOR`
   semantics and grapheme width.
2. `hack/terminal-qualification/pty_check.py` runs the real CLI in a PTY. It
   checks alternate-screen and cursor restoration, terminal modes, title
   control sequences, resize storms, UTF-8 validity and output bounds.
3. `hack/qualify-linux-terminals.sh` starts pinned xterm, Kitty and Alacritty
   packages under Xvfb, runs every supported size, records screenshots, then
   runs tmux and a delayed key-only SSH session.

The live matrix is qualification evidence for a named source revision. It is
not a scheduled CI job or a mandatory check for every release. Run it again
when a release widens a terminal claim, changes the Bubble Tea renderer or
input handling, or fixes a terminal-specific defect. The unit and safety
contracts remain in `make check`.

## Linux emulator matrix

Build the Linux CLI for Docker's architecture first. The runner rejects an
architecture mismatch and requires a dedicated empty evidence directory.

```sh
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 \
  go build -o /tmp/kubectl-memlens-linux ./cmd/kubectl-memlens

mkdir -m 700 /tmp/kube-memlens-terminal-evidence
TERMINAL_LINUX_ACKNOWLEDGE=run-and-remove-kube-memlens-linux-terminal-qualification \
TERMINAL_LINUX_CLI=/tmp/kubectl-memlens-linux \
TERMINAL_LINUX_KUBECONFIG=/path/to/dedicated-kubeconfig \
TERMINAL_LINUX_CONTEXT=kind-kube-memlens \
TERMINAL_LINUX_ARTIFACT_DIR=/tmp/kube-memlens-terminal-evidence \
  hack/qualify-linux-terminals.sh
```

The runner uses a digest-pinned Ubuntu base and exact package versions. It
removes the qualification image when the run exits. Its SSH server, host key,
client key and 80 ms loopback delay exist only inside the disposable container.

## PTY restoration and soak

Run the PTY checker against the exact candidate CLI. A full qualification uses
`--duration-seconds 1800`, `--refresh 1s` and an empty output path. Short runs
are useful while developing the harness but do not satisfy the 30-minute gate.

```sh
python3 hack/terminal-qualification/pty_check.py \
  --cli /path/to/kubectl-memlens \
  --kubeconfig /path/to/dedicated-kubeconfig \
  --context kind-kube-memlens \
  --collector-namespace kube-memlens \
  --profile apple-terminal \
  --terminal-version 2.15 \
  --term xterm-256color \
  --columns 180 --rows 50 \
  --duration-seconds 1800 \
  --refresh 1s \
  --exit-mode normal \
  --colour-mode native \
  --output /tmp/kube-memlens-terminal-evidence/soak.json
```

Run separate short cases for normal exit, Ctrl-C, SIGINT, SIGTERM,
`NO_COLOR`, low colour and a connection-error screen. The result contains no
raw output, credential path or cluster identifier.

## Evidence review

Before committing evidence:

- bind every row to the exact source commit and CLI SHA-256;
- retain terminal and operating-system versions;
- review screenshots for readable boundaries and sanitised workload names;
- keep only JSON summaries and representative PNGs;
- record unavailable terminals as unqualified rather than inferred support;
- verify that every temporary workload, container, key and image is gone.

Screen-reader conformance is outside this matrix. The table, JSON and YAML CLI
outputs remain the non-interactive accessibility path.
