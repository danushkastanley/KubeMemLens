#!/usr/bin/env bash
set -Eeuo pipefail

repository=${1:-danushkastanley/KubeMemLens}
environment=$(gh api "repos/${repository}/environments/release")
policies=$(gh api "repos/${repository}/environments/release/deployment-branch-policies")
rulesets=$(gh api "repos/${repository}/rulesets")
immutability=$(gh api "repos/${repository}/immutable-releases")

jq -e '
  .deployment_branch_policy == {protected_branches:false, custom_branch_policies:true} and
  any(.protection_rules[];
    .type == "required_reviewers" and (.reviewers | type == "array" and length > 0))
' <<< "${environment}" >/dev/null

jq -e '
  any(.branch_policies[]; .name == "v*" and .type == "tag") and
  any(.branch_policies[]; .name == "main" and .type == "branch")
' <<< "${policies}" >/dev/null

tag_ruleset_id=$(jq -r '
  first(.[] | select(.name == "release tag protection" and .target == "tag" and .enforcement == "active") | .id) // empty
' <<< "${rulesets}")
[ -n "${tag_ruleset_id}" ]

gh api "repos/${repository}/rulesets/${tag_ruleset_id}" | jq -e '
  .conditions.ref_name.include == ["refs/tags/v*"] and
  ([.rules[].type] | sort) == (["creation", "deletion", "non_fast_forward", "update"] | sort)
' >/dev/null

jq -e '.enabled == true' <<< "${immutability}" >/dev/null

echo 'release environment, tag and immutability settings passed'
