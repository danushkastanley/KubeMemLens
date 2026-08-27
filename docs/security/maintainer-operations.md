# Maintainer Security and Release Operations

This runbook defines the minimum operating path for repository security,
dependency and secret findings, private vulnerability reports, and release
handover. It does not create a support service-level agreement.

The named primary and backup owners are in [MAINTAINERS.md](../../MAINTAINERS.md).
Both roles must be filled before a v1 release. The backup is an active operator,
not an emergency credential or an unmonitored account.

## Account and repository controls

Each maintainer must use a separate GitHub account protected by a passkey or
hardware-backed multi-factor authentication. Do not share passwords, personal
access tokens, signing material, browser sessions, or recovery codes.

The live controls and verification commands are recorded in
[repository-security.md](../repository-security.md). Review them before a
release candidate and after any ownership, ruleset, workflow, or environment
change.

## Private vulnerability reporting drill

Exercise the path with a synthetic draft advisory before v1 and after a material
GitHub reporting-policy change:

1. Confirm private vulnerability reporting is enabled.
2. Create a draft advisory whose title starts `Synthetic reporting drill` and
   whose description states that it contains no vulnerability or sensitive data.
3. Have the receiving maintainer acknowledge it through the private advisory.
4. Record a synthetic severity, affected test version, decision, and handover.
5. Close the draft without requesting a CVE and without publishing it.
6. Retain only the date, participants by maintainer role, outcome, and GitHub
   advisory identifier. Do not retain advisory text or account details in logs.

Never test this path with a real secret, exploit, customer identifier, provider
resource, production log, or unredacted cluster response.

## Vulnerability response

1. The receiving maintainer acknowledges the report and checks that discussion
   remains inside the private advisory.
2. The primary classifies affected versions, required attacker access, impact,
   exploitability, and whether the supported contract must be narrowed.
3. Invite only the minimum maintainers and contributors needed to fix the issue.
4. Prepare the fix on the advisory's private fork when confidentiality is needed.
5. Add a regression test and run the relevant security, lifecycle, upgrade,
   rollback, and clean-consumer checks.
6. Agree disclosure timing with the reporter. Request a CVE only for a confirmed
   vulnerability when it improves user remediation.
7. Publish a new immutable version. Never overwrite a tag, image, chart, archive,
   checksum, signature, attestation, SBOM, or release asset.
8. Update release notes with every fixed vulnerability that already has a CVE or
   equivalent public identifier.

Follow the targets in [SECURITY.md](../../SECURITY.md). When a target cannot be
met, publish a safe mitigation or withdraw the affected support claim rather
than imply that the issue is resolved.

## Dependency findings

The primary security maintainer owns Dependabot, `govulncheck`, Trivy, image,
chart, and release-SBOM findings. The backup owns overdue review and release
handover.

1. Confirm reachability, affected build or runtime artefacts, fix availability,
   and the source advisory.
2. Treat confirmed critical or high reachable findings as release blockers.
3. Apply the smallest compatible update and retain the ordinary test and release
   gates. Do not batch an unrelated dependency migration into the fix.
4. For a false positive, unreachable result, or accepted risk, record the exact
   package, version, advisory, evidence, owner, expiry date, and support impact.
5. Re-open an accepted result when reachability, exposure, or upstream guidance
   changes. An expired exception blocks release.

Weekly Dependabot version updates are bounded to Go modules, GitHub Actions, and
the Docker build. Security updates remain enabled independently. The repository
does not auto-merge dependency pull requests.

## Secret findings

Secret scanning and push protection are preventive controls, not proof that a
value was never exposed.

1. Treat every alert as potentially live until its issuer confirms otherwise.
2. Revoke or rotate the credential before removing it from any location.
3. Determine scope from the provider audit trail without copying the secret into
   an issue, commit, log, or chat.
4. Review releases, packages, Actions artefacts, caches, forks, and Git history
   that could contain the value.
5. Record only the secret type, exposure window, affected surfaces, rotation
   confirmation, owner, and closure reason.
6. Do not rewrite public history as the only containment action. Coordinate any
   exceptional history rewrite separately after rotation.

## Release and emergency handover

The primary prepares the release-candidate evidence and the backup reviews the
release checklist, subject digests, workflow run, and draft assets. The protected
`release` environment supplies the manual approval boundary. Neither maintainer
approves a deployment they initiated when GitHub can enforce separation.

If the primary account is unavailable or suspected compromised, the backup:

1. pauses publication and changes the release environment reviewers;
2. checks repository rules, deploy keys, webhooks, Actions secrets, environments,
   packages, releases, advisories, and recent audit-visible changes;
3. revokes affected credentials and sessions;
4. restores controls through their normal GitHub settings or API paths; and
5. records a privacy-safe incident summary and the evidence required before
   publication resumes.

An unavailable backup blocks a release but does not justify sharing credentials
or bypassing immutable publication controls.
