"""Export an offline two-platform trace image twice and compare content digests.

No registry upload, kernel execution or runtime policy is involved. BuildKit runs
only COPY instructions against the assembler's public context and scratch base.
"""
import argparse
from datetime import datetime, timezone
import hashlib
import json
from pathlib import Path
import re
import subprocess

from manifest import VERSION, canonical, match, require


def sha(path):
    digest = hashlib.sha256()
    with path.open('rb') as stream:
        for block in iter(lambda: stream.read(1 << 20), b''):
            digest.update(block)
    return digest.hexdigest()


def context_files(context):
    result = {}
    for path in sorted(context.rglob('*')):
        require(not path.is_symlink() and (path.is_file() or path.is_dir()), 'invalid image input')
        if path.is_file():
            result[str(path.relative_to(context))] = sha(path)
    return result


def build(assembled, output, version, commit, epoch):
    require(match(VERSION, version) and len(version) <= 64 and re.fullmatch('[a-f0-9]{40}', commit) and
            type(epoch) is int and 0 <= epoch <= 2**32-1, 'invalid image build identity')
    context = assembled/'context'
    expected = json.loads((assembled/'context-files.json').read_bytes())
    require(context_files(context) == expected, 'image context changed after assembly')
    recipe = Path(__file__).resolve().parent/'Dockerfile'
    recipe_digest = sha(recipe)
    created = datetime.fromtimestamp(epoch, timezone.utc).isoformat(timespec='seconds').replace('+00:00', 'Z')
    output.mkdir(mode=0o700)
    results = []
    for attempt in (1, 2):
        archive = output/f'trace-image-{attempt}.oci.tar'
        metadata = output/f'build-{attempt}.json'
        command = ['docker', 'buildx', 'build', '--network=none', '--platform=linux/amd64,linux/arm64',
                   '--provenance=false', '--sbom=false', '--build-arg', f'SOURCE_DATE_EPOCH={epoch}',
                   '--label', 'org.opencontainers.image.source=https://github.com/danushkastanley/KubeMemLens',
                   '--label', 'org.opencontainers.image.revision='+commit,
                   '--label', 'org.opencontainers.image.version='+version,
                   '--label', 'org.opencontainers.image.created='+created,
                   '--label', 'io.kubememlens.trace.contract=1', '--metadata-file', str(metadata),
                   '--output', f'type=oci,dest={archive},tar=true,rewrite-timestamp=true',
                   '--file', str(recipe), str(context)]
        with (output/f'build-{attempt}.log').open('xb') as log:
            subprocess.run(command, stdout=log, stderr=subprocess.STDOUT, timeout=600, check=True)
        info = json.loads(metadata.read_bytes())
        digest = info.get('containerimage.digest')
        require(type(digest) is str and re.fullmatch('sha256:[a-f0-9]{64}', digest), 'image index digest unavailable')
        require(0 < archive.stat().st_size <= 1 << 30, 'image archive exceeds bound')
        results.append({'name': archive.name, 'size': archive.stat().st_size, 'sha256': sha(archive), 'indexDigest': digest})
    require(context_files(context) == expected and sha(recipe) == recipe_digest, 'image inputs changed during build')
    require(results[0]['indexDigest'] == results[1]['indexDigest'], 'image content reproduction failed')
    (output/'receipt.json').write_bytes(canonical({'schemaVersion': 1, 'releaseVersion': version, 'sourceCommit': commit,
        'epoch': epoch, 'dockerfileSHA256': recipe_digest, 'contextSHA256': sha(assembled/'context-files.json'),
        'images': results, 'indexReproduced': True, 'archivesIdentical': results[0]['sha256'] == results[1]['sha256'],
        'published': False, 'candidateExecuted': False}))
    print('Both platform images reproduced by OCI index digest; no upload or candidate execution.')


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--assembled', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--version', required=True)
    parser.add_argument('--commit', required=True)
    parser.add_argument('--epoch', type=int, required=True)
    args = parser.parse_args()
    build(args.assembled.resolve(), args.output.resolve(), args.version, args.commit, args.epoch)
