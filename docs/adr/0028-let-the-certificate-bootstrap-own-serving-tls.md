# ADR 0028: Let the certificate bootstrap own the serving TLS material

Date: 24 November 2026

Status: proposed for review; implemented in PR 385

## Context

The aggregated API needs a serving certificate, its private key, and a matching
`caBundle` on the APIService. Today `templates/extension-tls.yaml` creates all
three while the chart renders. It reads the existing Secret with `lookup` and
reuses it; when `lookup` returns nothing it calls `genCA` and `genSignedCert`.

`lookup` returns nothing whenever the chart is rendered without cluster access:
Argo CD, `helm template | kubectl apply`, and `helm install --dry-run=client`.
Two consecutive `helm template` runs differ in `ca.crt`, `tls.crt`, `tls.key`
and `caBundle` (reproduced on `main` at `5042878`). Under GitOps every sync
would therefore replace the Secret and the `caBundle`. The private key is also
part of the rendered manifests and, with the Helm CLI, of the Helm release
Secret. The chart README has stated "Helm CLI only" since PR 343; issues 266
(audit B-17) and 320 (audit N-12) ask for the structural fix.

Facts read from the repository that constrain the fix:

- The collector mounts the Secret as a required volume and reads the
  certificate files at start. Without the Secret its Pod stays in
  `ContainerCreating`.
- `memlens-cert-bootstrap` runs as a `post-install,post-upgrade,post-rollback`
  hook. It gets the Secret and fails if it is absent; it never creates one. Its
  Role allows `get` and `update` on that one Secret by name, and its
  ClusterRole allows `get` and `update` on the one APIService by name.
- With `helm install --wait`, post-install hooks run only after the collector is
  ready. A chart that shipped no Secret and kept the bootstrap as a post hook
  would never become ready.
- The Secret and the `caBundle` are part of the release manifest today. A
  chart version that stops rendering them makes `helm upgrade` delete the
  Secret and clear the `caBundle` of every existing install.

## Decision proposed

Add an install mode in which the chart renders no key material, and keep the
current mode as the default until the migration has been observed.

1. New value `extensionTLS.source`, either `chart` (default, today's behaviour)
   or `bootstrap`.
2. With `bootstrap`, the chart renders no Secret and an APIService without
   `caBundle`. Rendering is then deterministic.
3. With `bootstrap`, the bootstrap Job also runs before the workloads, as a
   `pre-install,pre-upgrade,pre-rollback` hook. It creates the Secret when it is
   absent and rotates it when it is due. The existing post hook keeps its job:
   patch the `caBundle` and wait for the collector to serve the new
   certificate.
4. `memlens-cert-bootstrap` learns to create the Secret. Its Role gains
   `create` on Secrets in the install namespace. Kubernetes cannot restrict
   `create` by resource name, so this is a wider grant than today, held only by
   a hook-scoped ServiceAccount that exists while the Job runs.
5. With `bootstrap`, the Secret is named `<extensionTLS.secretName>-bootstrap`.
   Helm deletes the chart-rendered Secret when a release switches source, and
   the collector must already have the other one to mount.
6. The chart refuses `extensionTLS.source=bootstrap` together with
   `namespace.create=true`, because a pre-install hook runs before the chart's
   Namespace exists.
7. The default stays `chart`. Changing the default, or removing `chart`, is a
   separate decision after `bootstrap` has been run on real installs.

In `bootstrap` mode the private key is no longer in the rendered manifests or
the Helm release Secret.

## Alternatives considered

- **Remove chart generation outright.** Smallest chart, but every existing
  install would have its Secret deleted and `caBundle` cleared on upgrade,
  which stops the aggregated API until the bootstrap runs, and cannot run under
  `--wait` if the collector restarts first. Rejected as the first step.
- **Render an empty placeholder Secret that the bootstrap fills.** Keeps the
  Role at `get` and `update` by name. A `kubernetes.io/tls` Secret must carry
  both keys, so the placeholder would need another type, and a client-side
  apply resets any key the manifest lists. Rejected: more moving parts than a
  `create` grant on a short-lived account.
- **Generate the certificate in the collector.** The collector's ServiceAccount
  would need to write Secrets and the APIService. Rejected: it widens the
  privileges of the one long-running, network-reachable component.
- **Require cert-manager.** Solves ownership and rotation, but adds a
  third-party dependency to every install. It could be offered later as a third
  `source`; it is not the default path.
- **Keep "Helm CLI only".** No risk, but GitOps installs stay unsupported.

## Consequences

- Two code paths in the chart until the default is settled. Each needs its own
  render check and kind run.
- GitOps tools that honour Helm hooks (Argo CD maps them to PreSync and
  PostSync) get the Secret before the workloads. A plain
  `helm template | kubectl apply` ignores hook ordering: the Job and the
  collector start together and the collector Pod waits for the Secret. That
  path also leaves the finished Job behind, and a later change to its spec
  needs the old Job deleted first. The README must say so.
- The APIService `caBundle` is written by the bootstrap and absent from the
  manifest. A GitOps tool may report that field as drift and must be told to
  ignore it; the README must give the setting.
- Rotation stays tied to install, upgrade and rollback. Scheduled rotation is
  issue 294 and is not decided here.

## What running it showed

Run on a disposable kind cluster with Kubernetes 1.37.0, for the implementation
in PR 385:

- The API server accepts an APIService with no `caBundle` and
  `insecureSkipTLSVerify: false`. It became available once the bootstrap wrote
  the `caBundle`.
- `helm upgrade` leaves a `caBundle` written by the bootstrap untouched when
  neither revision's manifest has one. No read failed during such an upgrade.
- Switching an existing release between `chart` and `bootstrap`, in either
  direction or by rollback, replaces the serving CA. Reads through the
  aggregated API failed for a few seconds to about half a minute each time.
- `helm template | kubectl apply`, done twice, converged without hook ordering
  and left the Secret and the `caBundle` unchanged on the second apply.
- `helm uninstall` leaves the bootstrap Secret behind, because it is not part
  of the release.

Still not run: Argo CD and Flux. How they report the bootstrap-written
`caBundle`, and whether Argo CD's PreSync phase runs after the namespace
exists, remain open.

## Verification, migration and rollback

Acceptance for the implementation:

- `helm template` twice with `extensionTLS.source=bootstrap` gives identical
  output; a render check in `hack/test-chart-value-guards.sh` holds that.
- kind: fresh install in each mode; upgrade from the current chart in `chart`
  mode; switch `chart` to `bootstrap` and back; rollback; uninstall. After each
  step the APIService is `Available` and a read through it succeeds.
- kind: `helm template | kubectl apply` twice in `bootstrap` mode, with
  `kubectl diff` showing no change to the Secret after the second apply.
- Unit tests in `internal/certbootstrap` for create-when-absent and for a
  Secret created by another writer between the read and the create.

Existing installs change nothing unless they set `extensionTLS.source`. Rolling
back to an older chart revision restores chart-rendered material; the post hook
then reconciles the `caBundle` as it does today.
