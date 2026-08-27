# Maintainers and Governance

## Maintainers and operating roles

| Role | Maintainer | GitHub | Responsibilities |
|---|---|---|---|
| Primary release and security maintainer | Danushka Stanley | [@danushkastanley](https://github.com/danushkastanley) | Product direction, code review, releases, security coordination, and community governance |
| Backup release and security maintainer | `legolas296` | [@legolas296](https://github.com/legolas296) | Independent release review, security-report handover, and repository recovery |

The backup role must be held by a real, consenting person using a separate
GitHub account. It cannot be represented by a shared credential, unattended
service account, or an invented project identity. Both named maintainers have
separate accepted administrator access; the live settings gate verifies that
access and the documented review controls before release.

## Decision making

KubeMemLens is currently maintainer-led while the contributor community forms.

- Routine changes are decided through issue and pull-request review.
- Material architecture, security, data, dependency, privilege, or compatibility decisions require a concise ADR or design issue before implementation.
- Decisions should favour evidence, least privilege, backwards compatibility, bounded resource use, and the smallest maintainable solution.
- When consensus is not available, the maintainer makes and documents the decision and its trade-offs.

The primary may merge ordinary changes after required automated checks. Release,
security, repository-policy, dependency-exception, and support-contract changes
require backup review once the role is filled. An unavailable backup delays
those operations; it does not authorise credential sharing or policy bypass.

## Becoming a maintainer

Maintainer invitations are based on sustained, constructive contributions; sound technical judgement; reliable review; respectful community participation; and commitment to the project's security and product boundaries. This document will evolve towards a multi-maintainer governance model as the community grows.

## Inactive maintainers

Maintainers may step down at any time. A future multi-maintainer project may mark a maintainer emeritus after prolonged inactivity through a documented pull request.
