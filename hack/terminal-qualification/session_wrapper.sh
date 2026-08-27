#!/usr/bin/env bash

set -u

state_file=${1:?state file is required}
shift
before=$(stty -g)
terminal_size=$(stty size)
"$@"
exit_code=$?
after=$(stty -g)
printf '%s|%s|%s|%s\n' "${before}" "${after}" "${terminal_size}" "${exit_code}" > "${state_file}"
exit "${exit_code}"
