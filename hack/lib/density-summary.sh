#!/usr/bin/env bash

# shellcheck disable=SC2154

write_summary() {
  local completed_at=$1
  local samples_json=${work_dir}/samples.json control_json=${work_dir}/canary-control.json
  local observed_json=${work_dir}/canary-observed.json

  jq -s '.' "${samples}" > "${samples_json}"
  jq -s '.' "${canary_control}" > "${control_json}"
  jq -s '.' "${canary_observed}" > "${observed_json}"
  jq -n \
    --arg outcome "${outcome}" --arg completedAt "${completed_at}" \
    --argjson churnRecoverySeconds "${churn_recovery_seconds}" \
    --argjson controlAgentRecoverySeconds "${control_agent_recovery_seconds}" \
    --argjson workerNodeRecoverySeconds "${worker_node_recovery_seconds}" \
    --argjson replacementExpectedPods "${workload_replacement_expected_pods}" \
    --argjson replacementObservedPods "${workload_replacement_observed_pods}" \
    --argjson replacementContainersBefore "${workload_replacement_resident_containers_before}" \
    --argjson replacementContainersAfter "${workload_replacement_resident_containers_after}" \
    --argjson disruptionRestarts "${disruption_unexplained_restarts}" \
    --argjson disruptionOOMKills "${disruption_oom_kills}" \
    --argjson apiStart "${api_baseline}" --argjson apiEnd "${api_steady_end}" \
    --slurpfile profile "${profile_path}" --slurpfile samples "${samples_json}" \
    --slurpfile control "${control_json}" --slurpfile observed "${observed_json}" \
    --slurpfile reliability "${reliability_summary}" '
    ($profile[0]) as $p | ($samples[0] // []) as $points | ($reliability[0] // {}) as $recovery |
    {schemaVersion:1, orchestrationOutcome:$outcome, completedAt:$completedAt,
     disruptionOperational:{unexplainedRestarts:$disruptionRestarts,oomKills:$disruptionOOMKills},
     workloadReplacement:{expectedPods:$replacementExpectedPods,observedPods:$replacementObservedPods,
       residentContainersBefore:$replacementContainersBefore,
       residentContainersAfter:$replacementContainersAfter},
     profile:{id:$p.id,digest:$p.profileDigest},workload:$p.workload,samples:$points,
     measurements:{
       agentScanMilliseconds:[$points[] | select(.phase == "steady") |
         .agents.scanMilliseconds | select(type == "number")],
       cliLatencyMilliseconds:[$points[] | select(.phase == "steady") |
         .cliLatencyMilliseconds | select(type == "number")],
       tuiLatencyMilliseconds:[$points[] | select(.phase == "steady") |
         .tuiLatencyMilliseconds | select(type == "number")],
       recoverySeconds:(if ($recovery.result // "") == "pass" then {
         workload:$churnRecoverySeconds,
         agent:([$controlAgentRecoverySeconds,$recovery.agentRecoverySeconds]|max),
         collector:$recovery.finalRecoverySeconds,
         node:([$workerNodeRecoverySeconds,$recovery.nodeRecoverySeconds]|max),
         api:$recovery.apiRecoverySeconds,partialRollout:$recovery.partialRolloutSeconds
       } else null end),
       components:{
         agent:{memoryLimitRatios:[$points[] | select(.phase == "steady") |
           .componentTelemetry.components.agent.maximumMemoryLimitRatio | select(type == "number")],
           cpuThrottlingRatios:[$points[] | select(.phase == "steady") |
             .componentTelemetry.components.agent.maximumCPUThrottlingRatio |
             select(type == "number")]},
         collector:{memoryLimitRatios:[$points[] | select(.phase == "steady") |
           .componentTelemetry.components.collector.maximumMemoryLimitRatio | select(type == "number")],
           cpuThrottlingRatios:[$points[] | select(.phase == "steady") |
             .componentTelemetry.components.collector.maximumCPUThrottlingRatio |
             select(type == "number")]}
       },
       agentPostFailures:[$points[] | select(.phase == "steady") | .agents |
         select(.available == true) | .postFailures],
       agentScanFailures:[$points[] | select(.phase == "steady") | .agents |
         select(.available == true) | .scanFailures],
       apiServer:(if $apiStart.available == true and $apiEnd.available == true then {
         errorDelta:(if $apiEnd.errors >= $apiStart.errors then $apiEnd.errors-$apiStart.errors else null end),
         rateLimitedDelta:(if $apiEnd.rateLimited >= $apiStart.rateLimited then
           $apiEnd.rateLimited-$apiStart.rateLimited else null end)
       } else null end),
       nodeMemoryPressureNodes:[$points[] | select(.phase == "steady") |
         .nodeMemoryPressureNodes | select(type == "number")],
       canary:{higherIsBetter:false,control:$control[0],observed:$observed[0]}
     },
     privacy:{clusterIdentifiersIncluded:false,workloadIdentifiersIncluded:false,
       rawMetricsIncluded:false,rawLogsIncluded:false},
     caveats:["This result supports only the selected profile and is not managed-provider evidence."]}
    ' \
    > "${artifact_dir}/density-soak-summary.json"
  chmod 600 "${artifact_dir}/density-soak-summary.json"
}
