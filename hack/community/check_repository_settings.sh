#!/usr/bin/env bash
set -euo pipefail

repository=${1:-danushkastanley/KubeMemLens}

for command in curl gh jq rg; do
  if ! command -v "$command" >/dev/null 2>&1; then
    echo "required command is unavailable: $command" >&2
    exit 1
  fi
done

backup_line=$(rg '^\| Backup release and security maintainer \|' MAINTAINERS.md)
backup_maintainer=$(sed -E 's/.*\[@([^]]+)\].*/\1/' <<<"$backup_line")
if [[ -z "$backup_maintainer" || "$backup_maintainer" == "$backup_line" ]]; then
  echo 'MAINTAINERS.md does not name the backup GitHub account' >&2
  exit 1
fi
if [[ "$backup_maintainer" == danushkastanley ]]; then
  echo 'backup maintainer must be independent from the primary maintainer' >&2
  exit 1
fi
if ! rg --quiet --fixed-strings "@${backup_maintainer}" .github/CODEOWNERS; then
  echo 'backup maintainer is absent from CODEOWNERS' >&2
  exit 1
fi

backup_permission=$(gh api "repos/${repository}/collaborators/${backup_maintainer}/permission" --jq '.permission')
if [[ "$backup_permission" != admin ]]; then
  echo "backup maintainer requires accepted admin access, found: $backup_permission" >&2
  exit 1
fi

main_rules=$(gh api "repos/${repository}/rules/branches/main")
for rule in deletion non_fast_forward pull_request required_status_checks; do
  if ! jq -e --arg rule "$rule" 'any(.[]; .type == $rule)' <<<"$main_rules" >/dev/null; then
    echo "main is missing required rule: $rule" >&2
    exit 1
  fi
done

jq -e '
  any(.[];
    .type == "pull_request" and
    .parameters.required_approving_review_count >= 1 and
    .parameters.dismiss_stale_reviews_on_push == true and
    .parameters.require_code_owner_review == true and
    .parameters.require_last_push_approval == true and
    .parameters.required_review_thread_resolution == true
  )
' <<<"$main_rules" >/dev/null || {
  echo 'main pull-request review policy is incomplete' >&2
  exit 1
}

expected_checks=(
  'Go checks'
  'Supply-chain checks'
  'Helm checks'
  'kind / Kubernetes 1.35.5'
  'kind / Kubernetes 1.36.1'
  'kind / Kubernetes 1.37.0'
  'CodeQL analysis'
)
for check in "${expected_checks[@]}"; do
  if ! jq -e --arg check "$check" '
    any(.[];
      .type == "required_status_checks" and
      .parameters.strict_required_status_checks_policy == true and
      any(.parameters.required_status_checks[]; .context == $check)
    )
  ' <<<"$main_rules" >/dev/null; then
    echo "main is missing required status check: $check" >&2
    exit 1
  fi
done

rulesets=$(gh api "repos/${repository}/rulesets")
release_ruleset_id=$(jq -r '
  [.[] | select(.name == "release tag protection" and .target == "tag")] |
  if length == 1 then .[0].id else empty end
' <<<"$rulesets")
if [[ -z "$release_ruleset_id" ]]; then
  echo 'one release tag protection ruleset was not found' >&2
  exit 1
fi
release_ruleset=$(gh api "repos/${repository}/rulesets/${release_ruleset_id}")
jq -e '
  .enforcement == "active" and
  .target == "tag" and
  (.conditions.ref_name.include == ["refs/tags/v*"]) and
  ([.rules[].type] | sort == ["creation", "deletion", "non_fast_forward", "update"])
' <<<"$release_ruleset" >/dev/null || {
  echo 'release tag ruleset differs from the required contract' >&2
  exit 1
}

environment=$(gh api "repos/${repository}/environments/release")
jq -e --arg primary danushkastanley --arg backup "$backup_maintainer" '
  .can_admins_bypass == false and
  any(.protection_rules[];
    .type == "required_reviewers" and
    .prevent_self_review == true and
    ([.reviewers[].reviewer.login] | index($primary) != null) and
    ([.reviewers[].reviewer.login] | index($backup) != null)
  ) and
  .deployment_branch_policy.protected_branches == false and
  .deployment_branch_policy.custom_branch_policies == true
' <<<"$environment" >/dev/null || {
  echo 'release environment review policy differs from the required contract' >&2
  exit 1
}

deployment_policies=$(gh api "repos/${repository}/environments/release/deployment-branch-policies")
jq -e 'any(.branch_policies[]; .name == "v*" and .type == "tag")' \
  <<<"$deployment_policies" >/dev/null || {
  echo 'release environment does not restrict deployments to v* tags' >&2
  exit 1
}

private_reporting=$(gh api "repos/${repository}/private-vulnerability-reporting")
jq -e '.enabled == true' <<<"$private_reporting" >/dev/null || {
  echo 'private vulnerability reporting is disabled' >&2
  exit 1
}

immutable_releases=$(gh api "repos/${repository}/immutable-releases")
jq -e '.enabled == true' <<<"$immutable_releases" >/dev/null || {
  echo 'immutable releases are disabled' >&2
  exit 1
}

workflow_permissions=$(gh api "repos/${repository}/actions/permissions/workflow")
jq -e '
  .default_workflow_permissions == "read" and
  .can_approve_pull_request_reviews == false
' <<<"$workflow_permissions" >/dev/null || {
  echo 'default workflow token permissions are too broad' >&2
  exit 1
}

repository_state=$(gh api "repos/${repository}")
jq -e '
  .security_and_analysis.dependabot_security_updates.status == "enabled" and
  .security_and_analysis.secret_scanning.status == "enabled" and
  .security_and_analysis.secret_scanning_push_protection.status == "enabled"
' <<<"$repository_state" >/dev/null || {
  echo 'repository dependency or secret protection is incomplete' >&2
  exit 1
}

labels=$(gh api "repos/${repository}/labels?per_page=100")
for label in compatibility diagnosis-feedback adopter-feedback question; do
  if ! jq -e --arg label "$label" 'any(.[]; .name == $label)' <<<"$labels" >/dev/null; then
    echo "repository is missing issue label: $label" >&2
    exit 1
  fi
done

scorecard=$(curl --silent --show-error --fail \
  "https://api.scorecard.dev/projects/github.com/${repository}")
jq -e '
  .score >= 7 and
  any(.checks[]; .name == "Branch-Protection" and .score >= 8) and
  any(.checks[]; .name == "Token-Permissions" and .score >= 8) and
  any(.checks[]; .name == "Dangerous-Workflow" and .score >= 8) and
  any(.checks[]; .name == "Pinned-Dependencies" and .score >= 8) and
  any(.checks[]; .name == "Signed-Releases" and .score >= 8) and
  any(.checks[]; .name == "Vulnerabilities" and .score >= 7)
' <<<"$scorecard" >/dev/null || {
  echo 'published Scorecard result is below the release threshold' >&2
  exit 1
}

best_practices_line=$(rg '^Best Practices project: ' docs/security/openssf-baseline.md)
best_practices_id=$(sed -nE 's#^Best Practices project: https://www\.bestpractices\.dev/(en/)?projects/([0-9]+)(/passing)?$#\2#p' <<<"$best_practices_line")
if [[ -z "$best_practices_id" || "$best_practices_id" == "$best_practices_line" ]]; then
  echo 'OpenSSF baseline does not record the Best Practices project URL' >&2
  exit 1
fi
best_practices=$(curl --silent --show-error --fail \
  "https://www.bestpractices.dev/projects/${best_practices_id}.json")
jq -e --arg repository "https://github.com/${repository}" '
  .repo_url == $repository and
  (.badge_level == "passing" or .badge_level == "silver" or .badge_level == "gold") and
  .badge_percentage_0 == 100 and
  .lost_passing_at == null
' <<<"$best_practices" >/dev/null || {
  echo 'OpenSSF Best Practices project has not achieved the Passing badge' >&2
  exit 1
}

dependabot_open=$(gh api "repos/${repository}/dependabot/alerts?state=open&per_page=100" --jq 'length')
secret_open=$(gh api "repos/${repository}/secret-scanning/alerts?state=open&per_page=100" --jq 'length')
printf 'repository community settings passed (open Dependabot: %s, open secrets: %s)\n' \
  "$dependabot_open" "$secret_open"
