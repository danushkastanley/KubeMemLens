#!/usr/bin/env bash

set -Eeuo pipefail

artifact_dir=${1:?artifact directory is required}
shift
[ "$#" -gt 0 ] || exit 2
[ -d "${artifact_dir}" ] || exit 2
[ "${TERMINAL_EMULATOR_DURATION_SECONDS:-}" = 1800 ] || exit 2

Xvfb :99 -screen 0 1920x1080x24 -nolisten tcp >/tmp/xvfb-soak.log 2>&1 &
xvfb_pid=$!
cleanup() {
  kill "${xvfb_pid}" 2>/dev/null || true
  wait "${xvfb_pid}" 2>/dev/null || true
}
trap cleanup EXIT
sleep 1
openbox >/tmp/openbox-soak.log 2>&1 &

/scripts/emulator_session.sh \
  kitty 180 50 \
  "${artifact_dir}/kitty-soak-180x50.json" \
  "${artifact_dir}/kitty-soak-180x50.png" \
  "$@"
