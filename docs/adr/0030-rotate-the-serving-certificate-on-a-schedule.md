# ADR 0030: Rotate the serving certificate on a schedule

Date: 25 November 2026

Status: proposed for review; implemented in the same pull request

## Context

The aggregated API's serving certificate is valid for 365 days by default. The
`memlens-cert-bootstrap` Job replaces it when it is within
`extensionTLS.rotateBefore` (30 days) of expiry, but the Job runs only as a Helm
hook on install, upgrade and rollback. A release that is not upgraded for a
year lets the certificate expire, and every read and every agent write then
fails. Nothing reported the expiry. Issue 294 (audit I-02) asks for scheduled
rotation and an expiry metric.

The hook's ServiceAccount, Role and ClusterRole are hooks too, so they exist
only while a hook Job runs. A scheduled Job needs an account that exists all
the time.

## Decision proposed

1. A CronJob, `kube-memlens-cert-rotation`, runs the same binary with the same
   arguments as the post hook, without `--force`. It is on by default.
2. The default schedule is weekly. A run only rotates when the certificate is
   inside the 30-day window, and a monthly schedule can put one run just before
   the window opens and the next after the certificate has expired.
3. The CronJob has its own ServiceAccount, Role and ClusterRole, which are
   ordinary release resources. They allow `get` and `update` on the one serving
   Secret and the one APIService, by name. They do not allow `create`, in
   either `extensionTLS.source` mode: if the Secret is missing, the run fails
   and the next install, upgrade or rollback creates it.
4. `extensionTLS.rotation.enabled=false` removes the CronJob and the account.
5. The collector reports the expiry of the mounted certificate as
   `kubememlens_extension_serving_certificate_not_after_seconds`, read from the
   file on each scrape. The optional PrometheusRule gains
   `KubeMemLensServingCertificateExpiring` at 14 days, with a runbook.

## Alternatives considered

- **Rotate inside the collector.** The collector would need to write its own
  Secret and the APIService. Rejected for the reason given in ADR 0028: it
  widens the privileges of the one long-running, network-reachable component.
- **Off by default.** No standing permission is added, but the defect stays
  for everyone who does not find the setting. The standing permission is two
  named objects, and the same permission already exists during every hook.
- **A monthly schedule, as the issue proposed.** Rejected: see point 2.
- **Let the CronJob create a missing Secret.** Kubernetes cannot limit `create`
  to one name, so this would be a standing namespace-wide grant.
- **Require cert-manager.** Adds a third-party dependency to every install.

## Consequences

- An account that can replace the serving key and the trusted CA for this API
  now exists for the life of the release, not only during hooks. Anyone who can
  create Pods in the install namespace can use it. The threat model records
  this.
- An existing release gains the CronJob and its RBAC on its next upgrade,
  including one that reuses stored values.
- A rotation still depends on the kubelet refreshing the mounted Secret before
  the Job's 150-second deadline, as it does in the hook today.
- A schedule that runs less often than `extensionTLS.rotateBefore` can miss
  the window. The chart cannot check that; the README and the runbook say so.
- With `extensionTLS.source=chart`, a later `helm rollback` restores the key
  material stored in that revision, as it did before. The post-rollback hook
  then rotates it if it is due.

## Verification, migration and rollback

Acceptance for the implementation:

- Render checks: the CronJob is present by default, absent when turned off or
  when the collector is disabled, and a schedule without five fields is
  refused.
- kind: install with a one-day certificate, start a run from the CronJob, and
  see a new certificate, an Available APIService, a working read and a later
  value of the expiry metric. Start another run and see nothing change.
- kind: upgrade from the chart on `main` with stored values and see the
  CronJob created and the certificate unchanged.
- Unit tests for the expiry reader, including a replaced file and an unreadable
  one.

Rolling back to an older chart removes the CronJob and its account. Nothing is
stored that needs migrating.
