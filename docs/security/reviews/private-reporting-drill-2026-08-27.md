# Private Vulnerability Reporting Drill — 27 August 2026

| Field | Result |
|---|---|
| Advisory | `GHSA-9r58-62g3-2v7p` |
| Content | Synthetic workflow exercise; no vulnerability, exploit, credential, identifier, or production data |
| Primary receipt | Passed |
| Backup handover | `@legolas296` has active admin access; the human acknowledgement drill remains pending |
| CVE request | None |
| Private fork | None |
| Publication | None |
| Final state | Closed privately at 12:26:46 UTC |

The GitHub API accepted the synthetic draft through the repository security-
advisory path and returned it as `draft`. The primary maintainer then closed it
six seconds later. Readback confirmed `state: closed`, a null CVE identifier,
and no publication.

No advisory body, authentication token, provider data, cluster data, or raw API
response is retained in the repository. Repeat the handover portion with the
backup maintainer before PROD-011 acceptance.
