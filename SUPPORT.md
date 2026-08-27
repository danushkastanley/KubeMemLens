# Support

KubeMemLens is currently an alpha, community-maintained project. Support is
best effort. There is no paid support, guaranteed response time, service-level
agreement, or production stability guarantee.

Before opening a request, check the [installation guide](docs/installation.md),
[compatibility contract](docs/compatibility.md), and existing issues. The
compatibility contract is the source of truth for supported and unsupported
environments.

## Where to ask

- Use the [support request form](https://github.com/danushkastanley/KubeMemLens/issues/new?template=support_request.yml)
  for installation and usage questions.
- Use the [bug report form](https://github.com/danushkastanley/KubeMemLens/issues/new?template=bug_report.yml)
  for reproducible incorrect behaviour.
- Use the [feature request form](https://github.com/danushkastanley/KubeMemLens/issues/new?template=feature_request.yml)
  for product proposals.
- Use the [compatibility report form](https://github.com/danushkastanley/KubeMemLens/issues/new?template=compatibility_report.yml)
  for runtime or mapping gaps.
- Use the private process in [SECURITY.md](SECURITY.md) for suspected
  vulnerabilities. Do not open a public support issue for them.

Maintainers may convert a support request into a bug, compatibility report, or
feature request when that route better preserves the evidence and decision.

## Safe diagnostic information

Include only the minimum needed to understand the request:

- exact KubeMemLens version or commit;
- Kubernetes version and installation method;
- provider family or self-managed, without account, project, subscription,
  cluster, node-pool, namespace, workload, or host names;
- node operating-system family, architecture, cgroup mode, and container
  runtime; and
- a short redacted outcome or error class.

Do not post credentials, kubeconfig content, tokens, certificates, provider
identifiers, Pod UIDs, container IDs, labels, image names, cgroup paths, raw
Kubernetes objects, or proprietary workload data. Prefer synthetic names and
the default-redacted capture path.

Maintainers may close a request that cannot be handled safely in public and ask
for a synthetic reproduction. Private vulnerability reporting is not a general
private support channel.

## Response boundary

Questions are reviewed as maintainer capacity permits. Acknowledgement and
resolution times are not guaranteed. Unsupported environments may receive
documentation or design guidance, but a support claim requires the recorded
qualification process in [docs/qualification.md](docs/qualification.md).
