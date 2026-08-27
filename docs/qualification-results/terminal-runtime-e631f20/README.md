# Terminal runtime qualification for `e631f20`

This record qualifies five exact Linux and remote terminal rows for the
application source commit `e631f2032bdc85624fee32e14478ee1e7a57d392`.
It does not claim support for a terminal that was unavailable or could not be
exercised by the host automation boundary.

## Result

| Row | Version | Result | Sizes or path |
| --- | --- | --- | --- |
| xterm on Ubuntu arm64 | 390 | Qualified | 80x24, 120x30, 180x50 |
| Kitty on Ubuntu arm64 | 0.32.2 | Qualified | 80x24, 120x30, 180x50 and a 30-minute 180x50 run |
| Alacritty on Ubuntu arm64 | 0.13.2 | Qualified | 80x24, 120x30, 180x50 |
| tmux on Ubuntu arm64 | 3.4 | Qualified | 120x30, 30 seconds |
| OpenSSH on Ubuntu arm64 | 9.6p1 | Qualified | 120x30, 30 seconds, 80 ms loopback delay |
| Apple Terminal on macOS arm64 | 2.15 | Unqualified | Installed, but terminal GUI automation was not permitted |
| Ghostty on macOS arm64 | 1.3.1 | Unqualified | Installed, but terminal GUI automation was not permitted |
| iTerm2 on macOS | Unreported | Unqualified | Not installed |
| Warp on macOS | Unreported | Unqualified | Not installed |

Unqualified is not a permanent rejection. Those macOS rows need a real manual
or permitted emulator run before the support claim can widen.

## Candidate binding

- Application source: `e631f2032bdc85624fee32e14478ee1e7a57d392`.
- Qualification tools: `ac8a9618d043b4b4015b8cf3ddfe5ae3e0c3b56f`.
- Darwin arm64 CLI SHA-256:
  `aa6481ab373ed4e7706ac277e44f12faa95639d08d43734f18284fafba0be202`.
- Linux arm64 CLI SHA-256:
  `7ecbc35abf7f2a505597a9b12fe95ad988ea4e7e6ca6f915eeb5c8d7580969ad`.
- Client host: macOS 26.6.2, Darwin 25.6.0, arm64.
- Linux emulator base: Ubuntu 24.04 arm64 under Xvfb 21.1.12.
- Live data path: local kind Kubernetes 1.35.5 with one-second refresh.

The [machine-readable matrix](terminal-matrix.json) binds each qualified row to
its result files.

## Long runs

The [PTY soak](pty/soak-1800s.json) ran for 1,800.137 seconds. It recorded
5,121,083 output bytes, an average of 2,844.829 bytes per second, and zero title
sequences. Alternate-screen state, cursor visibility, terminal modes and UTF-8
all restored or remained valid. Periodic keys and resize changes included a
below-minimum 39x9 state before returning to 180x50.

The [real Kitty soak](linux/kitty-soak-180x50.json) ran at 180x50 for 1,800
seconds. It accepted periodic navigation, sort, help and pause keys, retained
its title, captured the final frame and restored terminal mode on exit. The
[final screenshot](screenshots/kitty-soak-180x50.png) shows the live wide view
after the repeated input cycle.

Kitty printed non-fatal diagnostics because Bubble Tea v2.0.9 sends the legacy
xterm `modifyOtherKeys` setup alongside the Kitty keyboard protocol. Rendering,
ordinary-key input and restoration still passed. KubeMemLens does not depend on
the enhanced protocol.

## Other exercised paths

- Normal quit, Ctrl-C, SIGINT and SIGTERM each restored the alternate screen,
  cursor and terminal modes.
- `NO_COLOR` emitted no colour SGR sequences. The low-colour `vt100` path kept
  text labels, composition letters and the selection marker.
- A refused connection showed an honest error screen and restored the terminal
  on exit.
- The [live kind smoke](e2e/tui-smoke-summary.json) exercised 20 workload Pods,
  three namespaces, all supported sizes, paging, filtering, detail history,
  pause, manual refresh and read-only recommendations.
- Go tests covered Unicode grapheme width, below-minimum resize storms, focus,
  paging, selection across failure and recovery, and semantic monochrome
  output.

The first outer Kitty soak command reported a post-session shell argument error
after the passing JSON and screenshot had been written. The session itself
completed and restored cleanly. The runner now passes the container CLI
arguments as one array, and the evidence validator checks all twelve Linux
result files independently.

## Privacy and scope

No raw PTY stream, key, kubeconfig path, context name or cluster endpoint is
retained. Screenshots contain only disposable synthetic workload and node
names. The bundle makes no screen-reader conformance claim. Table, JSON and YAML
output remain the non-interactive accessibility path.

This is historical, version-bound evidence. It is not scheduled in CI and is
not required for every release. Re-run it when a release widens a terminal
claim or changes the renderer, input handling or restoration path.
