# Optional resource-metrics source

K137-005 adds an opt-in source for the Kubernetes Metrics API. Existing cgroup
collection, collector storage and the default CLI/TUI do not call it. Restricted
mode remains R3 work.

`client.NewResourceMetricsSource` reuses kubeconfig, context, timeout and namespace
options. It requires a Kubernetes API connection and one namespace. The lower
level `resourcemetrics.New` accepts a caller-owned REST configuration. Both use
the caller's credentials; neither grants access or queries an administrator's
collector on the caller's behalf. The chart adds no Metrics API permission.

## Discovery and availability

Each read discovers `metrics.k8s.io`, prefers advertised `v1`, and otherwise
selects advertised `v1beta1`. The selected version must expose namespaced Pod
metrics with list support. A denied, malformed or failed stable-version read
never triggers a beta fallback.

The result distinguishes available, missing-provider, forbidden, unavailable,
partial and stale. Missing-provider means the group discovery endpoint returned
404. A valid empty list means no observations were reported; it does not establish
that the namespace has no Pods. Provider coverage remains explicitly unknown.

Latest Metrics Server v0.9.0 currently registers only v1beta1, while released
Kubernetes metrics v0.37.0 includes v1 types. API availability is discovered rather
than inferred from Kubernetes version. Sources: [released v1 types](https://github.com/kubernetes/metrics/blob/v0.37.0/pkg/apis/metrics/v1/types.go),
[Metrics Server v0.9.0 API registration](https://github.com/kubernetes-sigs/metrics-server/blob/v0.9.0/pkg/api/install.go)
and its [APIService manifest](https://github.com/kubernetes-sigs/metrics-server/blob/v0.9.0/manifests/base/apiservice.yaml).

## Measurement contract

Observations retain namespace, Pod name, optional provider-reported Pod UID,
container name, served API version, UTC timestamp and the provider's time window.
An absent UID remains absent; a name alone does not prove continuity across Pod
replacement. Consumers must account for that uncertainty when joining sources.

CPU usage is expressed in nanocores. Memory is the reported working set; it does
not supply cgroup charge, composition, reclaim controls or volume usage. The
source performs no conversion into `MemoryBreakdown`.

A container missing CPU, memory, timestamp or a positive window is omitted and
the report becomes partial. Reported zero values remain real observations.
Old and excessive future timestamps retain their values with distinct row
freshness; reports containing them are stale or partial. Mixed fresh/stale rows
must be handled individually.

## Bounds and trust boundary

Defaults are a five-second whole-read deadline, two-minute maximum age,
five-second clock-skew allowance, 4 MiB per response, 500 Pods per page,
four pages, 2,000 Pods and 10,000 containers. Validated options can raise these
within fixed ceilings. Scientific quantity exponents and encoded quantity sizes
are bounded before parsing; accepted values fit signed 64-bit bytes or nanocores.

The source uses the configured TLS trust and credential transport, disables
implicit compression and rejects redirects. It validates namespace, identity,
version, kind and cursor consistency. A cross-namespace response or later access
denial discards earlier pages. Errors expose typed reasons, not raw provider
messages, API URLs or tokens. No response cache is shared between callers.

Metrics API permissions authorise workload identity and usage data. The report
retains those identities for its authorised consumer; it is not a redacted export.
The collector receives no Metrics API data and persists no new field.

## Local verification

```sh
E2E_RUN_RESOURCE_METRICS_SMOKE=true hack/e2e-kind.sh
```

The harness creates a disposable TLS API fixture and namespace-scoped reader,
verifies missing-provider, stable-v1 preference, forbidden namespace access and
v1beta1 transition, then removes both APIServices, credentials and the namespace.
Its fixture has a separate Docker context and is not included in product images.
Unit/race tests also cover partial usage, freshness, cancellation, timeout,
redirects, quantity/response bounds, repeated cursors and revocation between pages.

Reverting the optional adapter removes this capability without a storage
migration or change to deep-mode collection.
