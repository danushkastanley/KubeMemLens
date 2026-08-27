# Private Vulnerability Reporting Drill — 27 August 2026

| Field | Result |
|---|---|
| Primary advisory | `GHSA-9r58-62g3-2v7p` |
| Backup-handover advisory | `GHSA-wfhm-45q5-8jf6` |
| Content | Synthetic workflow exercise; no vulnerability, exploit, credential, identifier, or production data |
| Primary receipt | Passed |
| Backup handover | Passed; `@legolas296` acknowledged the private advisory at 14:54:11 UTC |
| CVE request | None |
| Private fork | None |
| Publication | None |
| Final state | Closed privately: primary at 12:26:46 UTC; handover at 14:54:47 UTC |

The GitHub API accepted the synthetic draft through the repository security-
advisory path and returned it as `draft`. The primary maintainer then closed it
six seconds later. Readback confirmed `state: closed`, a null CVE identifier,
and no publication.

No advisory body, authentication token, provider data, cluster data, or raw API
response is retained in the repository. The second synthetic advisory verified
that the backup maintainer could open and acknowledge a private report. Repeat
the handover after a material reporting-policy or maintainer-access change.
