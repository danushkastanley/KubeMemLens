#!/usr/bin/env bash
set -Eeuo pipefail
profile=${VOLUME_QUALIFICATION_PROFILE:-hack/volume-qualification/profiles/kind-csi-137.json}
if [ "${1:-}" = --dry-run ]; then
  [ "$#" -eq 1 ] || { echo 'dry run accepts no other arguments' >&2; exit 1; }
  python3 - "${profile}" <<'PY'
import json,sys
sys.path.insert(0,'hack/volume-qualification')
from common import load
from profile import validate
p=validate(load(sys.argv[1]))
print(json.dumps({'scope':'local-reference','profile':p['id'],'digest':p['profileDigest'],'measurement':p['measurement'],'budgets':p['budgets'],'createsResources':False,'managedProviderQualified':False},indent=2))
PY
  exit 0
fi
[ "$#" -eq 0 ] || { echo 'only --dry-run is supported' >&2; exit 1; }
[ "${VOLUME_QUALIFICATION_ACKNOWLEDGE:-}" = create-and-remove-local-csi-fixture ] || { echo 'set VOLUME_QUALIFICATION_ACKNOWLEDGE=create-and-remove-local-csi-fixture' >&2; exit 1; }
export VOLUME_QUALIFICATION_PROFILE=${profile}
export NODE_CONTEXT_ACKNOWLEDGE=create-and-remove-node-context-kind
export NODE_CONTEXT_VERIFY_INGESTION=true NODE_CONTEXT_VERIFY_VOLUME_STATS=true NODE_CONTEXT_VOLUME_HEALTH_PROFILE=alpha
export NODE_CONTEXT_CLUSTER=${VOLUME_QUALIFICATION_CLUSTER:-kube-memlens-node-context-volume-qualification}
export NODE_CONTEXT_ARTIFACT_DIR=${VOLUME_QUALIFICATION_ARTIFACT_DIR:?VOLUME_QUALIFICATION_ARTIFACT_DIR is required}
exec bash hack/verify-node-context-kind.sh
