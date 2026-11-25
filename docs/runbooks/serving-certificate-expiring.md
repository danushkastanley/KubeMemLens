# Serving Certificate Expiring

This alert means the aggregated API's serving certificate expires within 14 days. When it expires the Kubernetes API server stops trusting the collector, and every `kubectl memlens` read and every agent write fails.

The certificate is rotated by a Job on install, upgrade and rollback, and by the `kube-memlens-cert-rotation` CronJob once a week. A run rotates only when the certificate is within `extensionTLS.rotateBefore` of its expiry, 30 days by default. This alert therefore means the scheduled rotation has not worked.

1. Check the CronJob and its last Job:
   `kubectl -n kube-memlens get cronjob kube-memlens-cert-rotation` and
   `kubectl -n kube-memlens get jobs`.
   If the CronJob is missing, `extensionTLS.rotation.enabled` is `false`, or the collector is disabled.
2. Read the logs of a failed Job: `kubectl -n kube-memlens logs job/<name>`.
   A Job that waits for the rotated certificate and times out usually means the collector Pod is not ready or a NetworkPolicy blocks the Job from the collector Service.
3. Rotate now by starting a run by hand:
   `kubectl -n kube-memlens create job --from=cronjob/kube-memlens-cert-rotation cert-rotation-manual`.
   If the CronJob is turned off, run `helm upgrade` with the same values; the upgrade hook rotates a certificate that is due.
4. Confirm the new expiry: `kubectl memlens status` succeeds, and `kubememlens_extension_serving_certificate_not_after_seconds` moves forward within a few minutes, once the kubelet has refreshed the mounted Secret.

If `extensionTLS.rotation.schedule` runs less often than `extensionTLS.rotateBefore`, a run can fall on each side of the window and miss it. Keep the schedule more frequent than the window.
