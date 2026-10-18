# Test an unpublished development build

`hack/node-qualification/run_development_provider.py` runs the same provider
protocol as the [signed candidate command](node-context-provider-execution.md),
using an explicitly reviewed local development signature. This permits testing
an unpublished build without creating a release tag. It does not grant release
qualification or provider support.

The proposal, target approval, clean tool checkout, fixed profiles, measurement
budgets, recovery, machine replacement, NetworkPolicy, production CLI and owned
Kubernetes cleanup requirements are unchanged. Provider infrastructure creation
and teardown remain separate operations. The command cannot provision a cluster.

## Prepare and review the inputs

Build the chart, CLI archives and multi-platform OCI archive from the exact
product source commit. Keep source/build records with the private proposal.
The image must contain the standard five executables for both `linux/amd64` and
`linux/arm64`; a single-platform image is insufficient. Each Linux CLI archive
must contain the same CLI bytes as its image. Include the operator host's CLI
archive as well. The OCI version label must be `dev`; the build date must equal
the source commit timestamp. The tool commit may differ from the product commit,
subject to the existing source and chart checks in proposal validation.

The bundle is a flat directory containing only the declared payloads and:

- `development-manifest.json`: the contract below.
- `development-manifest.sigstore.json`: a Cosign bundle signing those exact
  manifest bytes with a dedicated local development key.

The manifest has exactly these fields:

| Field | Value |
| --- | --- |
| `schemaVersion` | Integer `1` |
| `authority` | `local-development` |
| `sourceCommit` | Full 40-character Git commit matching the proposal |
| `sourceTreeDigest` | `production_source(sourceCommit)` from `source_digest.py` |
| `version` | `dev` |
| `buildDate` | Source commit timestamp as RFC 3339 with seconds and timezone |
| `imageDigest` | SHA-256 digest of the multi-platform OCI index |
| `chart` | Payload entry for the chart archive |
| `imageArchive` | Payload entry for the OCI archive |
| `cliArchives` | Entries keyed by `linux_amd64`, `linux_arm64` and, if needed, `darwin_amd64` or `darwin_arm64` |

Each payload entry contains exactly `name`, `size` and `sha256`. The name must be
a unique safe basename; size is the positive byte count, and digests include
`sha256:`. The chart limit is 20 MiB, OCI archive limit 4 GiB, and each CLI archive
limit 128 MiB. The CLI archive reader and OCI inspector additionally enforce
member type, path, decompression and executable inventory bounds.

Keep the private signing key outside the bundle. Review and independently pin
the manifest digest, public verification-key digest and executable Cosign digest.
The public key must come from that review, not from an untrusted bundle. Local
signing uses a signing configuration with no remote signing, transparency-log or
timestamp services. The executable integration check demonstrates this flow with
temporary keys:

```sh
python3 hack/node-qualification/check_development_signature.py \
  --cosign /absolute/path/to/cosign
```

This check uses synthetic archives and real local cryptography; it is not a
product build, runtime test or provider qualification. CI runs it with the pinned
Cosign version. It rejects a changed manifest, a different valid public key and
a changed payload, and removes its temporary keys and files.

## Invoke the experiment

Supply the same proposal, profile, plan digest, architecture, output directory,
replacement slot and two acknowledgements as the signed candidate command.
Replace `--candidate-bundle`, `--candidate-tag` and `--image-archive` with:

```sh
  --development-bundle /absolute/private/development-bundle \
  --development-manifest-digest "${reviewed_manifest_digest}" \
  --development-key /absolute/private/development.pub \
  --development-key-digest "${reviewed_key_digest}" \
  --cosign /absolute/path/to/cosign \
  --cosign-digest "${reviewed_cosign_digest}" \
  --acknowledge-development run-unpublished-development-candidate
```

The entry point verifies the signature, source binding, all payloads, both image
platforms and proposal binaries before executing candidate code or contacting
the target. Its bounded local signature verification explicitly omits a public
transparency-log requirement. That exception belongs only to this development
authority; the signed release verifier is unchanged.

## Interpret the results

Every JSON file in `evidence/`, including artefacts, measurements, failures,
cleaned observations and evaluations, has a development envelope:
`scope: development-provider-experiment`, `authority: local-development`,
`qualified: false`, `releaseQualificationGranted: false`. The underlying result
is in `observation`. Release evidence evaluators and cleanup finalisers reject
this envelope. Retain it when sharing or archiving results; stripping it does
not establish release authority.

The signature binds bytes to a reviewed local key. It does not establish a
release workflow identity, public transparency, build reproduction or independent
review. A compromised signing key or an incorrectly reviewed pin can authorise
untrusted inputs; use a dedicated key and inspect source/build records before
approving a run. The runner does not execute the verified inputs until proposal
validation and authority checks have passed.

Exit status and cleanup behaviour match the signed candidate command. Passing
measurements still leave cloud cleanup pending. Failed budgets and interruptions
retain development evidence and attempt owned Kubernetes cleanup. No outcome
promotes these results into release qualification.
