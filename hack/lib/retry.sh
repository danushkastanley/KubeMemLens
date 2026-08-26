#!/usr/bin/env bash

retry_to_file() {
  local attempts=$1
  local interval_seconds=$2
  local output=$3
  shift 3

  local attempt=1
  while [ "${attempt}" -le "${attempts}" ]; do
    if "$@" > "${output}"; then
      return 0
    fi
    if [ "${attempt}" -eq "${attempts}" ]; then
      return 1
    fi
    sleep "${interval_seconds}"
    attempt=$((attempt + 1))
  done
}

retry_capture() {
  local attempts=$1 interval_seconds=$2 attempt=1 value
  shift 2

  while [ "${attempt}" -le "${attempts}" ]; do
    if value=$("$@"); then
      printf '%s' "${value}"
      return 0
    fi
    if [ "${attempt}" -eq "${attempts}" ]; then
      return 1
    fi
    sleep "${interval_seconds}"
    attempt=$((attempt + 1))
  done
}
