#!/usr/bin/env bash

set -Eeuo pipefail

artifact_dir=${1:?artifact directory is required}
shift
[ "$#" -gt 0 ] || exit 2
[ -d "${artifact_dir}" ] || exit 2

Xvfb :99 -screen 0 1920x1080x24 -nolisten tcp >/tmp/xvfb.log 2>&1 &
xvfb_pid=$!
cleanup() {
  kill "${xvfb_pid}" 2>/dev/null || true
  wait "${xvfb_pid}" 2>/dev/null || true
}
trap cleanup EXIT
sleep 1
openbox >/tmp/openbox.log 2>&1 &

for emulator in xterm kitty alacritty; do
  for size in 80x24 120x30 180x50; do
    columns=${size%x*}
    rows=${size#*x}
    /scripts/emulator_session.sh \
      "${emulator}" "${columns}" "${rows}" \
      "${artifact_dir}/${emulator}-${size}.json" \
      "${artifact_dir}/${emulator}-${size}.png" \
      "$@"
  done
done
