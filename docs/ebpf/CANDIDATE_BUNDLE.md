# File/cache candidate bundle

Status: bundle construction remains an offline operation. One exact candidate
has separate acceptance for bounded local testing; see
[local qualification](FILE_CACHE_LOCAL_QUALIFICATION.md). Building or signing
another bundle does not grant execution authority.

`prototype/trace/cmd/build-filecache-bundle` packages the four reproduced file/cache
objects, their source snapshot, build record and SDK policy patch. It signs each
canonical review manifest with Ed25519 and creates a local OCI layout. It cannot
load, publish or install a programme, or create an execution acceptance policy.

The OCI manifests use the [OCI 1.1 artifact layout](https://github.com/opencontainers/image-spec/blob/v1.1.1/manifest.md#guidelines-for-artifact-usage):
an empty JSON configuration, an explicit artifact type and one BPF object layer.
The fixed annotations bind kind and architecture. Runtime validation requires the
exact canonical shape; URLs, arbitrary layers, embedded data and registry fallback
are not accepted even when the enclosing review manifest has a valid signature.

## Build and signature identity

Run the offline programme builder first. The bundle builder checks its pinned
builder/upstream identities, source snapshot hashes, both object copies and the
current SDK patch hash before signing. It rejects duplicate or missing artifact
identities, changed source, differing object copies and unrecognised build records.
Its output directory must be new. Private keys must be separate, regular files
without group or other permissions. Existing keys and bundles are never replaced.

```sh
go -C prototype/trace run ./cmd/build-filecache-bundle keygen \
  --key /private/review/signing-key.pem
go -C prototype/trace run ./cmd/build-filecache-bundle build \
  --build /private/review/reproduced-objects \
  --sdk-patch /checkout/prototype/trace/worker/sdk-policy.patch \
  --key /private/review/signing-key.pem \
  --output /private/review/new-bundle
```

Each signed review manifest binds the object digest, OCI manifest digest, kind,
architecture, upstream SDK commit, builder digest and SDK patch digest.
`sourceSHA256` is the digest of the exact retained `build.json`, including its
source/header inventory and object identities. The original source snapshot is
included for review. It is not a claim that a self-reported build record alone
proves source reproduction; retain the actual compiler execution evidence too.

`candidate-index.json` records the public key digest and four review-manifest
digests with status `unapproved`. `BUILD_COMPLETE` is written last and means only
that packaging completed. Neither file grants execution authority. Ed25519 is
deterministic: the same source record, objects, patch and key produce identical
bundle file contents. This is not a reproducible worker-image claim.

## Runtime trust boundary

`filecache.Verifier` receives its public key and accepted digest map independently
from installation configuration. Its `Load` method never consults the candidate
index or bundled public key for authority. It verifies the accepted signature
before resolving object/OCI digests beneath a retained installation directory.
File reads are bounded, non-blocking and regular-file-only; final symlinks and
intermediate paths escaping that directory fail. Loaded objects are owned copies.

The [installation library](WORKER_INSTALLATION.md) now validates a signed engine
release, independent programme acceptance and their common stream identities.
The runtime still needs its reviewed executable/image, installed acceptance
configuration and completed node integration. The bundle does not include a worker image
or complete SDK/header redistribution material. Complete licence/vulnerability
inventory, privilege and seccomp qualification, source/digest/patch acceptance,
real kernel tests and later independent review gates remain required.
