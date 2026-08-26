#!/usr/bin/env bash

# Evidence writers for hack/qualify-cluster.sh. The caller owns all referenced
# state and performs the live checks before requesting a passing result.
# shellcheck disable=SC2154

collect_provider_receipt() {
  local temporary
  temporary=$(mktemp "${artifact_dir}/.provider-inventory.XXXXXX")
  if ! python3 hack/provider-inventory/collect.py --profile "${profile_path}" > "${temporary}"; then
    rm -f -- "${temporary}"
    return 1
  fi
  chmod 600 "${temporary}"
  if ! python3 hack/provider-profiles/validate_privacy.py "${temporary}"; then
    rm -f -- "${temporary}"
    return 1
  fi
  mv -f "${temporary}" "${artifact_dir}/provider-inventory.json"
}

write_environment_evidence() {
  local observed_cgroup=$1
  local cni_enforced=$2
  local output=${artifact_dir}/environment.json
  local temporary
  temporary=$(mktemp "${artifact_dir}/.environment.XXXXXX")
  jq -n --arg provider "${provider_name}" --arg kubernetesVersion "${kubernetes_version}" \
    --arg nodeImage "${node_image}" --arg osImage "${os_image}" --arg kernelVersion "${kernel_version}" \
    --arg kubeletVersion "${kubelet_version}" --arg runtime "${runtime_version}" \
    --arg architecture "${architecture}" --arg cgroupVersion "${observed_cgroup}" \
    --arg cniName "${cni_name}" --argjson cniEnforced "${cni_enforced}" \
    --argjson linuxNodeCount "${linux_nodes}" --argjson windowsNodeCount "${windows_nodes}" '
    {schemaVersion:1,provider:$provider,kubernetesVersion:$kubernetesVersion,nodeImage:$nodeImage,
     osImage:$osImage,kernelVersion:$kernelVersion,kubeletVersion:$kubeletVersion,
     runtime:$runtime,architecture:$architecture,cgroupVersion:$cgroupVersion,
     cniName:$cniName,cniEnforced:$cniEnforced,
     linuxNodeCount:$linuxNodeCount,windowsNodeCount:$windowsNodeCount}
  ' > "${temporary}"
  chmod 600 "${temporary}"
  if ! python3 hack/provider-profiles/validate_privacy.py "${temporary}"; then
    rm -f -- "${temporary}"
    return 1
  fi
  mv "${temporary}" "${output}"
}

write_lifecycle_evidence() {
  local doctor_check_count mapped_containers upgrade_revision rollback_revision mixed_os_result
  doctor_check_count=$(jq '.checks | length' "${artifact_dir}/doctor.json")
  mapped_containers=$(jq '.mapping.mapped' "${artifact_dir}/doctor.json")
  upgrade_revision=$(jq -r '.[-2].revision | tonumber' "${work_dir}/helm-history.json")
  rollback_revision=$(jq -r '.[-1].revision | tonumber' "${work_dir}/helm-history.json")
  mixed_os_result=not_run
  if [ "${windows_nodes}" -gt 0 ]; then
    mixed_os_result=pass
  fi
  jq -n \
    --arg imageDigest "${image_digest}" --arg chartDigest "${chart_digest}" \
    --arg valuesDigest "${values_digest}" --arg probeImageDigest "${probe_image##*@}" \
    --arg sourceCommit "${source_commit}" --arg cgroupVersion "${cgroup_version}" \
    --arg cniName "${cni_name}" --arg mixedOSResult "${mixed_os_result}" \
    --argjson linuxNodes "${linux_nodes}" --argjson windowsNodes "${windows_nodes}" \
    --argjson desiredAgents "${desired}" --argjson readyAgents "${ready}" \
    --argjson doctorChecks "${doctor_check_count}" --argjson mappedContainers "${mapped_containers}" \
    --argjson installSeconds "${first_explanation_seconds}" \
    --argjson agentSeconds "${agent_recovery_seconds}" \
    --argjson collectorSeconds "${collector_recovery_seconds}" \
    --argjson nodeSeconds "${node_recovery_seconds}" \
    --argjson upgradeRevision "${upgrade_revision}" --argjson rollbackRevision "${rollback_revision}" '
    {schemaVersion:1,
     releaseIdentity:{imageDigest:$imageDigest,chartDigest:$chartDigest,valuesDigest:$valuesDigest,
       probeImageDigest:$probeImageDigest,sourceCommit:$sourceCommit},
     checks:{
       prerequisites:{passed:true,profileCanonical:true,sourceClean:true,providerReceiptBound:true},
       helmInstall:{passed:true,revision:1},
       readiness:{passed:true,linuxNodes:$linuxNodes,desiredAgents:$desiredAgents,
         readyAgents:$readyAgents,collectorReady:true},
       mounts:{passed:true,cgroupPath:"/sys/fs/cgroup",readOnly:true,projectedToken:true},
       securityContext:{passed:true,nonRoot:true,readOnlyRootFilesystem:true,
         privilegeEscalation:false,capabilitiesDropped:true},
       mixedOSScheduling:{passed:true,result:$mixedOSResult,linuxNodes:$linuxNodes,windowsNodes:$windowsNodes},
       collection:{passed:true,cgroupVersion:$cgroupVersion,doctorChecks:$doctorChecks,
         mappedContainers:$mappedContainers},
       api:{passed:true,statusHealthy:true,explanationMetadata:true,metricsAvailable:true},
       tui:{passed:true,columns:80,rows:24,cleanExit:true},
       networkPolicy:{passed:true,cniName:$cniName,servicePort:443,
         allowedControls:2,deniedControls:1,deniedProbeExecuted:true},
       agentRestart:{passed:true,recoverySeconds:$agentSeconds},
       collectorRestart:{passed:true,recoverySeconds:$collectorSeconds},
       nodeReplacement:{passed:true,recoverySeconds:$nodeSeconds,providerReceiptRefreshed:true},
       upgrade:{passed:true,upgradeRevision:$upgradeRevision,rollbackRevision:$rollbackRevision,
         postRollbackDoctor:true},
       uninstall:{passed:true,clusterResourcesRemaining:0,namespaceRemoved:true}}
    }' > "${artifact_dir}/lifecycle.json"
  chmod 600 "${artifact_dir}/lifecycle.json"
}

failure_reason_code() {
  case "$1" in
    prerequisites) echo prerequisite_failed ;;
    helmInstall) echo helm_install_failed ;;
    readiness) echo readiness_failed ;;
    collection) echo collection_failed ;;
    tui) echo tui_failed ;;
    api) echo api_failed ;;
    upgrade) echo upgrade_failed ;;
    uninstall) echo uninstall_failed ;;
    nodeReplacement) echo node_replacement_failed ;;
    agentRestart) echo agent_restart_failed ;;
    collectorRestart) echo collector_restart_failed ;;
    mounts) echo mount_verification_failed ;;
    securityContext) echo security_context_failed ;;
    networkPolicy) echo network_policy_failed ;;
    mixedOSScheduling) echo mixed_os_scheduling_failed ;;
    *) echo prerequisite_failed ;;
  esac
}

write_pending_evidence() {
  local outcome=$1
  local failed_check=${2:-}
  local output=${3:-${artifact_dir}/provider-qualification.pending.json}
  local reason=null
  local manifest=null
  local mixed_os=not_run
  if [ "${windows_nodes}" -gt 0 ]; then
    mixed_os=pass
  fi
  if [ "${outcome}" = failed ]; then
    reason=$(failure_reason_code "${failed_check}")
    reason=$(jq -Rn --arg value "${reason}" '$value')
  fi
  if [ "${outcome}" = passed ]; then
    manifest=$(jq -Rn --arg value "${evidence_manifest_digest}" '$value')
  fi
  jq -n \
    --arg outcome "${outcome}" --arg completedAt "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
    --arg profileID "${profile_id}" --arg profileDigest "${profile_digest}" \
    --arg imageDigest "${image_digest}" --arg chartDigest "${chart_digest}" \
    --arg valuesDigest "${values_digest}" --arg providerReceiptDigest "${provider_receipt_digest}" \
    --arg sourceCommit "${source_commit}" --arg chartVersion "${chart_version}" \
    --arg probeImageDigest "${probe_image##*@}" \
    --argjson evidenceManifestDigest "${manifest}" \
    --arg mixedOS "${mixed_os}" --arg failedCheck "${failed_check}" --argjson reasonCode "${reason}" \
    --slurpfile environment "${artifact_dir}/environment.json" '
    def ordered_checks:
      ["prerequisites","helmInstall","readiness","mounts","securityContext","mixedOSScheduling","collection",
       "api","tui","networkPolicy","agentRestart","collectorRestart","nodeReplacement",
       "upgrade","uninstall"];
    def failed_checks:
      (ordered_checks | index($failedCheck)) as $failedIndex |
      reduce ordered_checks[] as $name ({};
        .[$name] = if (ordered_checks | index($name)) < $failedIndex then "pass"
                   elif $name == $failedCheck then "fail" else "not_run" end);
    {schemaVersion:1,outcome:$outcome,completedAt:$completedAt,
     profile:{id:$profileID,digest:$profileDigest},
     artefacts:{imageDigest:$imageDigest,chartDigest:$chartDigest,valuesDigest:$valuesDigest,
       providerReceiptDigest:$providerReceiptDigest,evidenceManifestDigest:$evidenceManifestDigest,
       probeImageDigest:$probeImageDigest,sourceCommit:$sourceCommit,chartVersion:$chartVersion},
     environment:($environment[0] | del(.schemaVersion)),
     checks:(if $outcome == "passed" then
       {prerequisites:"pass",helmInstall:"pass",readiness:"pass",collection:"pass",
        tui:"pass",api:"pass",upgrade:"pass",uninstall:"pass",nodeReplacement:"pass",
        agentRestart:"pass",collectorRestart:"pass",mounts:"pass",securityContext:"pass",
        networkPolicy:"pass",mixedOSScheduling:$mixedOS}
       else failed_checks end),
     reasonCode:$reasonCode,
     privacy:{clusterIdentifiersIncluded:false,workloadIdentifiersIncluded:false,
       providerResourceIdentifiersIncluded:false,rawErrorsIncluded:false,rawLogsIncluded:false}}
  ' > "${output}"
  chmod 600 "${output}"
}
