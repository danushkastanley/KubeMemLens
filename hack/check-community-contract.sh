#!/usr/bin/env bash
set -euo pipefail

required_files=(
  CODE_OF_CONDUCT.md
  CONTRIBUTING.md
  LICENSE
  MAINTAINERS.md
  NOTICE
  SECURITY.md
  SUPPORT.md
  .github/CODEOWNERS
  .github/dependabot.yml
  .github/ISSUE_TEMPLATE/adopter_feedback.yml
  .github/ISSUE_TEMPLATE/bug_report.yml
  .github/ISSUE_TEMPLATE/feature_request.yml
  .github/ISSUE_TEMPLATE/support_request.yml
  .github/workflows/codeql.yml
  .github/workflows/scorecard.yml
  docs/community-feedback.md
  docs/repository-security.md
  docs/security/maintainer-operations.md
  docs/security/openssf-baseline.md
  docs/security/openssf-passing-assessment.md
  docs/security/reviews/private-reporting-drill-2026-08-27.md
  hack/community/check_repository_settings.sh
)

for path in "${required_files[@]}"; do
  if [[ ! -s "$path" ]]; then
    echo "community contract file is missing or empty: $path" >&2
    exit 1
  fi
done

require_text() {
  local path=$1
  local text=$2
  if ! rg --fixed-strings --quiet "$text" "$path"; then
    echo "$path is missing required text: $text" >&2
    exit 1
  fi
}

require_text SECURITY.md 'private security advisory form'
require_text SECURITY.md 'not a contractual service-level agreement'
require_text SECURITY.md 'at 60 days require an explicit mitigation'
require_text SUPPORT.md 'There is no paid support, guaranteed response time, service-level'
require_text SUPPORT.md 'Private vulnerability reporting is not a general'
require_text MAINTAINERS.md 'Primary release and security maintainer'
require_text MAINTAINERS.md 'Backup release and security maintainer'
require_text docs/repository-security.md 'Branch-Protection'
require_text docs/repository-security.md 'at least 7.0'
require_text docs/repository-security.md 'zero reachable'
require_text docs/repository-security.md 'Best Practices target is the Passing badge'
require_text docs/security/openssf-passing-assessment.md "\`know_secure_design\` | Met"
require_text docs/security/openssf-passing-assessment.md "\`test_most\` | Unmet"
require_text docs/security/maintainer-operations.md 'Synthetic reporting drill'
require_text docs/security/maintainer-operations.md 'Never overwrite a tag'
require_text docs/security/reviews/private-reporting-drill-2026-08-27.md 'Final state | Closed privately'
require_text .goreleaser.yml '      - NOTICE'
require_text .github/ISSUE_TEMPLATE/adopter_feedback.yml 'feedback, not a new provider-support qualification'
require_text .github/ISSUE_TEMPLATE/support_request.yml 'This is not a suspected security vulnerability.'

license_digest=$(sha256sum LICENSE | awk '{print $1}')
if [[ "$license_digest" != cfc7749b96f63bd31c3c42b5c471bf756814053e847c10f3eb003417bc523d30 ]]; then
  echo 'LICENSE differs from the canonical Apache-2.0 text; project notices belong in NOTICE' >&2
  exit 1
fi

if ! rg --quiet '^Copyright 2026 KubeMemLens contributors$' NOTICE; then
  echo 'NOTICE is missing the project copyright notice' >&2
  exit 1
fi

scorecard_workflow=.github/workflows/scorecard.yml
require_text "$scorecard_workflow" 'permissions: read-all'
require_text "$scorecard_workflow" 'security-events: write'
require_text "$scorecard_workflow" 'id-token: write'
require_text "$scorecard_workflow" 'publish_results: true'
if rg --quiet 'pull_request_target' "$scorecard_workflow"; then
  echo 'Scorecard workflow must not run with pull_request_target' >&2
  exit 1
fi

while IFS= read -r action; do
  reference=${action##*@}
  reference=${reference%% *}
  if [[ ! "$reference" =~ ^[0-9a-f]{40}$ ]]; then
    echo "Scorecard workflow action is not pinned by full commit SHA: $action" >&2
    exit 1
  fi
done < <(rg --only-matching 'uses: [^[:space:]]+' "$scorecard_workflow")

codeql_workflow=.github/workflows/codeql.yml
require_text "$codeql_workflow" 'security-events: write'
require_text "$codeql_workflow" 'languages: go'
while IFS= read -r action; do
  reference=${action##*@}
  reference=${reference%% *}
  if [[ ! "$reference" =~ ^[0-9a-f]{40}$ ]]; then
    echo "CodeQL workflow action is not pinned by full commit SHA: $action" >&2
    exit 1
  fi
done < <(rg --only-matching 'uses: [^[:space:]]+' "$codeql_workflow")

for ecosystem in gomod github-actions docker; do
  require_text .github/dependabot.yml "package-ecosystem: $ecosystem"
done

for form in \
  .github/ISSUE_TEMPLATE/adopter_feedback.yml \
  .github/ISSUE_TEMPLATE/support_request.yml; do
  require_text "$form" 'Privacy'
  require_text "$form" 'required: true'
done

if rg --quiet --ignore-case \
  '(^|[^[:alnum:]])(24/7 support|guaranteed support|production certified|certified production)([^[:alnum:]]|$)' \
  --glob '*.md' \
  --glob '!local-docs/**' \
  .; then
  echo 'public documentation contains an unsupported service or certification claim' >&2
  exit 1
fi

echo 'community operations contract passed'
