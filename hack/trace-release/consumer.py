"""Verify a private snapshot's signature, metadata, payloads and Helm rendering.

No install, registry write, candidate execution or runtime acceptance is performed.
Scanning completeness and build reproduction need their separate retained evidence.
"""
import hashlib
import os
from pathlib import Path
import re
import tempfile

from bundle import open_directory, read_small, snapshot
from manifest import canonical, fields, require
from metadata import document, verify_provenance, verify_sbom
from render import verify_chart
from signatures import Cosign, verify_manifest
from sbom import inventories, verify_files
from tools import Tool


def payload_request(manifest):
    return {'schemaVersion': 1, 'releaseVersion': manifest.version,
            'sourceCommit': manifest.source_commit, 'imageDigest': manifest.image_digest,
            'payloads': [{'role': item.role, 'name': item.payload.name,
                          'size': item.payload.size, 'sha256': item.payload.sha256}
                         for item in manifest.artefacts]}


def payload_receipt(raw, manifest):
    value = document(raw, 4 << 20)
    expected = {'schemaVersion': 1, 'releaseVersion': manifest.version,
                'sourceCommit': manifest.source_commit, 'imageDigest': manifest.image_digest,
                'chartImage': 'ghcr.io/danushkastanley/kube-memlens-trace@'+manifest.image_digest,
                'payloadAgreement': True, 'authenticated': False, 'runtimeExecuted': False}
    fields(value, (*expected, 'engineDigest', 'programmeIndexDigest', 'inventories'))
    require(canonical({key: value[key] for key in expected}) == canonical(expected),
            'payload inspector returned a mismatched result')
    for key in ('engineDigest', 'programmeIndexDigest'):
        require(type(value[key]) is str and re.fullmatch(r'sha256:[a-f0-9]{64}', value[key]),
                'payload inspector returned an invalid identity')
    inventories(value['inventories'])
    return value


def verify_metadata(directory, manifest, builder):
    opened = open_directory(directory)
    results = []
    try:
        for item in manifest.artefacts:
            raw = read_small(opened, item.provenance.name, 1 << 20)
            item.provenance.verify(raw)
            provenance = verify_provenance(raw, manifest, item, builder)
            sboms = []
            for sbom in item.sboms:
                raw = read_small(opened, sbom.blob.name, 16 << 20)
                sbom.blob.verify(raw)
                sboms.append(verify_sbom(raw, item.payload, sbom.platform, manifest.version))
            results.append({'provenance': provenance, 'sboms': sboms})
    finally:
        os.close(opened)
    return results


def verify_bundle(source, destination, tool_specs, trust):
    destination = Path(destination).absolute()
    require(set(tool_specs) == {'cosign', 'inspector', 'helm', 'chart-checker'},
            'complete external verifier tools required')
    manifest = snapshot(source, destination)
    with tempfile.TemporaryDirectory(prefix='kml-trace-consumer-') as temporary:
        private = Path(temporary)
        tools = {name: Tool.copy(path, digest, private, name)
                 for name, (path, digest) in tool_specs.items()}
        # Authenticate before interpreting provenance or executing even trusted
        # parsers/renderers on payload content. No candidate binary is executed.
        authenticated = verify_manifest(destination, manifest,
                                        Cosign(tools['cosign'].path, tools['cosign'].sha256), trust)
        metadata = verify_metadata(destination, manifest, authenticated.builder)
        code, raw = tools['inspector'].run([str(destination)], private,
                                           canonical(payload_request(manifest)), maximum=4 << 20, timeout=120)
        require(code == 0, 'trace payload inspection failed')
        payloads = payload_receipt(raw, manifest)
        verified_files = verify_file_metadata(destination, manifest, payloads['inventories'])
        chart = next(item.payload for item in manifest.artefacts if item.role == 'chart')
        rendered = verify_chart(destination/chart.name, manifest, tools['helm'], tools['chart-checker'], private)
        # Recheck the complete snapshot after tools have consumed it. This also
        # makes a changed manifest, subject list or metadata a failed invocation.
        final = snapshot(destination, private/'rechecked')
        require(final.original == manifest.original, 'snapshot changed during verification')
        opened = open_directory(private/'rechecked')
        try:
            signature = read_small(opened, 'trace-release.sigstore.json', 4 << 20)
        finally:
            os.close(opened)
        require(hashlib.sha256(signature).hexdigest() == authenticated.signature_sha256,
                'signature changed during verification')
        return {'schemaVersion': 1, 'manifestSHA256': hashlib.sha256(manifest.original).hexdigest(),
                'authority': authenticated.authority, 'signatureVerified': True,
                'signatureSHA256': authenticated.signature_sha256,
                'trustRootSHA256': authenticated.trust_sha256,
                'releaseVersion': manifest.version, 'sourceCommit': manifest.source_commit,
                'metadataRelationships': metadata, 'fileInventories': verified_files,
                'payloads': {key: value for key, value in payloads.items() if key != 'inventories'}, 'chart': rendered,
                'tools': {name: tool.sha256 for name, tool in tools.items()},
                'installationPerformed': False, 'scannerCompletenessVerified': False,
                'buildReproductionVerified': False, 'resourceQualificationGranted': False}


def verify_file_metadata(directory, manifest, rows):
    expected = inventories(rows)
    opened = open_directory(directory)
    results = []
    try:
        for item in manifest.artefacts:
            for sbom in item.sboms:
                raw = read_small(opened, sbom.blob.name, 16 << 20)
                sbom.blob.verify(raw)
                checked = verify_files(raw, expected[(item.role, sbom.platform)])
                results.append({'role': item.role, 'platform': sbom.platform, **checked})
    finally:
        os.close(opened)
    return results
