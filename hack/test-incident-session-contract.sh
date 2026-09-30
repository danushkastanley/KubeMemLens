#!/usr/bin/env bash
set -Eeuo pipefail
umask 077
python3 hack/test_verify_incident_sessions.py
incident_work_dir=$(mktemp -d "${TMPDIR:-/tmp}/kube-memlens-incident-contract.XXXXXX")
trap 'rm -rf -- "${incident_work_dir}"' EXIT
render() {
  helm template kube-memlens charts/kube-memlens "$@" |
    sed -E 's/^([[:space:]]+(ca.crt|tls.crt|tls.key|caBundle):).*/\1 "<generated>"/'
}
render > "${incident_work_dir}/default.yaml"
render --set incidentSessions.enabled=false > "${incident_work_dir}/disabled.yaml"
cmp "${incident_work_dir}/default.yaml" "${incident_work_dir}/disabled.yaml"
for value in 'incidentSessions.enabled=true' 'incidentSessions.namespaces[0]=team-a' 'incidentSessions.enabled=true,incidentSessions.namespaces[0]=INVALID' 'incidentSessions.enabled=true,incidentSessions.namespaces[0]=team-a,incidentSessions.namespaces[1]=team-a' 'incidentSessions.enabled=true,incidentSessions.namespaces[0]=team-a,collector.enabled=false'; do
  if render --set "$value" > "${incident_work_dir}/invalid.yaml" 2> "${incident_work_dir}/error.txt"; then
    echo 'invalid incident session profile rendered' >&2; exit 1
  fi
done
render --set incidentSessions.enabled=true --set 'incidentSessions.namespaces[0]=team-a' --set 'incidentSessions.namespaces[1]=team-b' > "${incident_work_dir}/enabled.yaml"
go run ./hack/incident-session-contract "${incident_work_dir}/default.yaml" "${incident_work_dir}/enabled.yaml"
