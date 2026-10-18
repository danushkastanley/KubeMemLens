"""Scan an inspected payload set and derive its unsigned release manifest.

The caller signs the resulting manifest separately. This command never publishes
or installs, and its provenance records local build evidence without a SLSA claim.
"""
import argparse
from dataclasses import dataclass
from datetime import datetime, timezone
import hashlib
import os
from pathlib import Path
import tempfile

from bundle import copy_blob, open_directory, read_small, write_new
from consumer import payload_receipt
from manifest import Blob, Manifest, PAYLOAD_LIMITS, ROLES, SBOM_PLATFORMS, canonical, fields, require
from metadata import BUILD_TYPE, SOURCE, document, verify_provenance
from sbom import bind_scan, inventories
from signatures import LOCAL_BUILDER
from tools import Tool


@dataclass(frozen=True)
class Identity:
    version: str
    source_commit: str
    image_digest: str


def blob(name, data):
    return {'name': name, 'size': len(data), 'sha256': hashlib.sha256(data).hexdigest()}


def instant():
    return datetime.now(timezone.utc).isoformat(timespec='seconds').replace('+00:00', 'Z')


def bounded(path, maximum):
    opened = open_directory(path.parent)
    try:
        return read_small(opened, path.name, maximum)
    finally:
        os.close(opened)


def finalise(payloads, request_path, evidence_path, output, inspector_spec, syft_spec):
    started = instant()
    raw = bounded(request_path, 16384)
    request = document(raw, 16384)
    fields(request, ('schemaVersion', 'releaseVersion', 'sourceCommit', 'imageDigest', 'payloads'))
    identity = Identity(request['releaseVersion'], request['sourceCommit'], request['imageDigest'])
    require(request['schemaVersion'] == 1 and type(request['payloads']) is list and
            len(request['payloads']) == len(ROLES), 'invalid payload build request')
    descriptors = []
    for role, row in zip(ROLES, request['payloads']):
        fields(row, ('role', 'name', 'size', 'sha256'))
        require(row['role'] == role, 'invalid build role order')
        descriptors.append(Blob.read({key: row[key] for key in ('name', 'size', 'sha256')}, PAYLOAD_LIMITS[role]))
    require(len({item.name for item in descriptors}) == len(ROLES), 'duplicate build payload')
    # Build evidence is caller-owned, bounded public JSON, never arbitrary logs.
    evidence_raw = bounded(evidence_path, 512 << 10)
    evidence = document(evidence_raw, 512 << 10)
    expected_tools = {'hack/trace-release/'+path.name: hashlib.sha256(path.read_bytes()).hexdigest()
                      for path in sorted(Path(__file__).resolve().parent.glob('*.py'))}
    require(evidence.get('buildToolSources') == expected_tools, 'finalisation tools differ from evidence')
    output.mkdir(mode=0o700)
    directory = open_directory(payloads)
    try:
        for item in descriptors:
            copy_blob(directory, item, output)
    finally:
        os.close(directory)
    with tempfile.TemporaryDirectory(prefix='kml-trace-finalise-') as temporary:
        private = Path(temporary)
        inspector = Tool.copy(*inspector_spec, private, 'inspector')
        syft = Tool.copy(*syft_spec, private, 'syft')
        code, inspected = inspector.run([str(output)], private, canonical(request), maximum=4 << 20, timeout=120)
        require(code == 0, 'payload inspection failed before scanning')
        checked = payload_receipt(inspected, identity)
        files = inventories(checked['inventories'])
        # Relative scan inputs prevent local path disclosure in source metadata.
        config = private/'scan-config.yaml'
        write_new(private, config.name, b'check-for-app-update: false\nparallelism: 2\n')
        artefacts = []
        for role, payload in zip(ROLES, descriptors):
            sboms = []
            for platform in SBOM_PLATFORMS[role]:
                suffix = platform.replace('/', '-')
                args = ['scan', ('oci-archive:' if role == 'image' else 'file:')+payload.name,
                        '--config', str(config), '--source-name', payload.name,
                        '--source-version', identity.version, '--quiet', '-o', 'spdx-json']
                if role == 'image':
                    args += ['--platform', platform]
                code, scan = syft.run(args, private, maximum=16 << 20, timeout=180, working_directory=output, scratch_file_limit=128 << 20)
                require(code == 0, 'payload scanner failed')
                encoded = bind_scan(scan, payload, platform, identity.version, files[(role, platform)])
                name = role+'-'+suffix+'.sbom.json'
                write_new(output, name, encoded)
                sboms.append({'platform': platform, 'blob': blob(name, encoded)})
            # This local builder does not mint GitHub-release provenance. The
            # protected workflow needs its own authenticated builder identity.
            provenance = {'_type': 'https://in-toto.io/Statement/v1',
                'subject': [{'name': payload.name, 'digest': {'sha256': payload.sha256}}],
                'predicateType': 'https://slsa.dev/provenance/v1', 'predicate': {
                    'buildDefinition': {'buildType': BUILD_TYPE,
                        'externalParameters': {'source': SOURCE, 'sourceCommit': identity.source_commit,
                            'releaseVersion': identity.version, 'traceContract': 1, 'role': role},
                        'internalParameters': {'operation': 'scan and finalise local payloads', 'localBuildEvidence': evidence},
                        'resolvedDependencies': [
                            {'uri': SOURCE, 'digest': {'gitCommit': identity.source_commit}},
                            {'uri': 'file:local-build-evidence.json', 'digest': {'sha256': hashlib.sha256(evidence_raw).hexdigest()}},
                            {'uri': 'file:syft', 'digest': {'sha256': syft.sha256},
                             'annotations': {'upstreamRelease': 'https://github.com/anchore/syft/releases/tag/v1.49.0'}},
                            {'uri': 'file:inspect-trace-release', 'digest': {'sha256': inspector.sha256}}]},
                    'runDetails': {'builder': {'id': LOCAL_BUILDER},
                                   'metadata': {'startedOn': started, 'finishedOn': instant()}}}}
            encoded = canonical(provenance)
            require(len(encoded) <= 1 << 20, 'provenance exceeds bound')
            name = role+'.provenance.json'
            write_new(output, name, encoded)
            artefacts.append({'role': role, 'payload': {'name': payload.name, 'size': payload.size, 'sha256': payload.sha256},
                              'sboms': sboms, 'provenance': blob(name, encoded)})
        value = {'schemaVersion': 1, 'releaseVersion': identity.version, 'sourceCommit': identity.source_commit,
                 'traceContract': 1, 'image': {'repository': 'ghcr.io/danushkastanley/kube-memlens-trace',
                                            'digest': identity.image_digest}, 'artefacts': artefacts}
        manifest = Manifest.read(canonical(value))
        for item in manifest.artefacts:
            verify_provenance((output/item.provenance.name).read_bytes(), manifest, item, LOCAL_BUILDER)
        # Scanner tools must not leave hidden cache or metadata in the asset set.
        expected = set(manifest.asset_names()) - {'trace-release.json', 'trace-release.sigstore.json', 'release-subjects.txt'}
        require({path.name for path in output.iterdir()} == expected, 'unexpected finalisation output')
        write_new(output, 'release-subjects.txt', manifest.subjects())
        write_new(output, 'trace-release.json', manifest.original)
    print('Unsigned manifest and exact subjects assembled; independent signing and consumer verification required.')


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ('payloads', 'request', 'evidence', 'output', 'inspector', 'syft'):
        parser.add_argument('--'+name, required=True, type=Path)
    for name in ('inspector', 'syft'):
        parser.add_argument('--'+name+'-sha256', required=True)
    args = parser.parse_args()
    finalise(args.payloads.resolve(), args.request.resolve(), args.evidence.resolve(), args.output.resolve(),
             (args.inspector.resolve(), args.inspector_sha256), (args.syft.resolve(), args.syft_sha256))
