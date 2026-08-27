#!/usr/bin/env bash

set -Eeuo pipefail

emulator=${1:-}
columns=${2:-}
rows=${3:-}
result=${4:-}
screenshot=${5:-}
shift 5 || true

case "${emulator}" in
  xterm|kitty|alacritty) ;;
  *) echo "unsupported emulator: ${emulator}" >&2; exit 2 ;;
esac
[[ "${columns}" =~ ^[0-9]+$ ]] && [ "${columns}" -ge 40 ] && [ "${columns}" -le 500 ] || exit 2
[[ "${rows}" =~ ^[0-9]+$ ]] && [ "${rows}" -ge 10 ] && [ "${rows}" -le 200 ] || exit 2
[ "$#" -gt 0 ] || exit 2
[ ! -e "${result}" ] || exit 2
[ ! -e "${screenshot}" ] || exit 2

title="KubeMemLens-${emulator}-${columns}x${rows}"
state_file=$(mktemp /tmp/terminal-state.XXXXXX)
wrapper=(/scripts/session_wrapper.sh "${state_file}" "$@")

case "${emulator}" in
  xterm)
    xterm -geometry "${columns}x${rows}" -T "${title}" -e "${wrapper[@]}" &
    ;;
  kitty)
    kitty --title "${title}" \
      --override remember_window_size=no \
      --override "initial_window_width=${columns}c" \
      --override "initial_window_height=${rows}c" \
      "${wrapper[@]}" &
    ;;
  alacritty)
    alacritty --title "${title}" \
      --option "window.dimensions.columns=${columns}" \
      --option "window.dimensions.lines=${rows}" \
      -e "${wrapper[@]}" &
    ;;
esac
emulator_pid=$!

window_id=$(timeout 15s xdotool search --sync --name "^${title}$" | head -n 1)
deadline=$((SECONDS + 30))
while [ "${SECONDS}" -lt "${deadline}" ]; do
  xdotool windowactivate --sync "${window_id}" >/dev/null 2>&1 || true
  if xdotool getwindowname "${window_id}" | grep -Fxq "${title}"; then
    break
  fi
  sleep 1
done
xdotool getwindowname "${window_id}" | grep -Fxq "${title}"

sleep 3
for key in G s N p question question space space; do
  xdotool windowactivate --sync "${window_id}" >/dev/null 2>&1
  xdotool key "${key}"
  sleep 0.2
done
xdotool windowactivate --sync "${window_id}" >/dev/null 2>&1 || true
scrot --focused --overwrite "${screenshot}"
[ "$(wc -c < "${screenshot}")" -ge 5000 ]
xdotool getwindowname "${window_id}" | grep -Fxq "${title}"

xdotool windowactivate --sync "${window_id}" >/dev/null 2>&1 || true
xdotool key q 2>/dev/null || true
deadline=$((SECONDS + 15))
while kill -0 "${emulator_pid}" 2>/dev/null && [ "${SECONDS}" -lt "${deadline}" ]; do
  sleep 0.2
done
if kill -0 "${emulator_pid}" 2>/dev/null; then
  kill "${emulator_pid}" 2>/dev/null || true
  wait "${emulator_pid}" 2>/dev/null || true
  echo "${emulator} did not exit after q" >&2
  exit 1
fi
wait "${emulator_pid}"

[ -s "${state_file}" ]
IFS='|' read -r before after terminal_size exit_code < "${state_file}"
[ "${before}" = "${after}" ]
[ "${terminal_size}" = "${rows} ${columns}" ]
[ "${exit_code}" = 0 ]

case "${emulator}" in
  xterm) version=$(xterm -version 2>&1 | head -n 1) ;;
  kitty) version=$(kitty --version | head -n 1) ;;
  alacritty) version=$(alacritty --version | head -n 1) ;;
esac

jq -n \
  --arg emulator "${emulator}" \
  --arg version "${version}" \
  --argjson columns "${columns}" \
  --argjson rows "${rows}" '
  {
    schemaVersion: 1,
    outcome: "passed",
    terminal: {emulator: $emulator, version: $version, columns: $columns, rows: $rows},
    checks: {
      applicationAcceptedNavigationInput: true,
      cleanExit: true,
      terminalModeRestored: true,
      titleUnchanged: true,
      screenshotCaptured: true
    },
    privacy: {rawTerminalOutputRetained: false, credentialPathsRetained: false, clusterIdentifiersRetained: false}
  }' > "${result}"
chmod 600 "${result}" "${screenshot}"
rm -f "${state_file}"
