"""Assemble public engine/programme payloads from reproduced local build inputs.

No signing, installation, runtime acceptance or publication is performed. Inputs
must be caller-owned build outputs; inspection still gates downstream use.
"""
import argparse
import hashlib
import json
from pathlib import Path
import shutil
import subprocess

from manifest import SHA256, canonical, match, require
from pack import copy_public_tree, pack_tree


def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def copy_file(source, target, mode=0o444):
    require(source.is_file() and not source.is_symlink() and not target.exists(), 'invalid public file input')
    shutil.copyfile(source, target)
    target.chmod(mode)


def assemble(source, workers, launchers, programmes, reproduced_programmes, verification, upstream, output, epoch):
    output.mkdir(mode=0o700)
    context = output/'context'; context.mkdir(mode=0o755)
    engine = context/'engine'; engine.mkdir(mode=0o755)
    worker = json.loads((workers/'build.json').read_bytes())
    launcher = json.loads((launchers/'build.json').read_bytes())
    require(worker['builder'] == 'golang:1.27.1-alpine@sha256:cf6fca6641884b8433441b2b0652976f975e1d0fdd26d177eaaf8596087f3125' and
            worker['builder'] == launcher['builder'] and worker['buildFlags'] == launcher['buildFlags'] and
            worker['sourceSHA256'] == launcher['sourceSHA256'] and
            worker['sdkReceiptSHA256'] == launcher['sdkReceiptSHA256'], 'binary builds disagree')
    for name, digest in worker['sourceSHA256'].items():
        require(type(name) is str and not Path(name).is_absolute() and '..' not in Path(name).parts and
                (source/name).resolve().is_relative_to(source) and match(SHA256, digest), 'invalid source identity')
        require(sha(source/name) == digest, 'source differs from reproduced build')
    require(sha(source/'prototype/trace/worker/.sdk/.kml-source-receipt.json') == worker['sdkReceiptSHA256'],
            'SDK receipt differs from build')
    for architecture in ('amd64', 'arm64'):
        directory = engine/('linux-'+architecture); directory.mkdir(mode=0o755)
        for records, root, name in [(worker['workers'], workers, 'memlens-filecache-worker'),
                                     (launcher['artifacts'], launchers, 'memlens-trace')]:
            record = records[architecture]
            prefix = 'worker' if name == 'memlens-filecache-worker' else 'memlens-trace'
            require(record['first'] == f'{prefix}-{architecture}-1' and
                    record['repeat'] == f'{prefix}-{architecture}-2' and match(SHA256, record['sha256']),
                    'invalid reproduced binary identity')
            first, repeat = root/record['first'], root/record['repeat']
            require(sha(first) == sha(repeat) == record['sha256'] and first.stat().st_size == record['bytes'],
                    'binary reproduction differs from record')
            copy_file(first, directory/name, 0o555)
    require((programmes/'build.json').read_bytes() == (reproduced_programmes/'build.json').read_bytes(),
            'signed programme build record differs from reproduction')
    programme_record = json.loads((programmes/'build.json').read_bytes())
    require(len(programme_record['objects']) == 6, 'six reproduced programmes required')
    identities = set()
    for item in programme_record['objects']:
        identity = (item['kind'], item['architecture'])
        require(item['kind'] in ('files', 'cache', 'oom') and item['architecture'] in ('amd64', 'arm64') and
                identity not in identities and match(SHA256, item['sha256']) and
                item['path'] == item['kind']+'-'+item['architecture']+'-1/program.bpf.o', 'invalid programme identity')
        identities.add(identity)
        first = reproduced_programmes/item['path']
        repeat = reproduced_programmes/(item['kind']+'-'+item['architecture']+'-2')/'program.bpf.o'
        require(sha(first) == sha(repeat) == item['sha256'], 'programme reproduction differs from record')
    for name, digest in programme_record['headerSHA256'].items():
        require(type(name) is str and name.startswith('include/') and '..' not in Path(name).parts and
                (upstream/name).resolve().is_relative_to(upstream) and match(SHA256, digest), 'invalid header identity')
        require(sha(upstream/name) == digest, 'compiler headers differ from build')
    copy_public_tree(programmes, context/'programmes')
    copy_public_tree(source/'prototype/trace/testdata/reference', engine/'reference')
    public = engine/'verification'; public.mkdir(mode=0o755)
    for name in ('engine-release.json', 'engine-release.sig', 'engine-public-key.bin'):
        copy_file(verification/name, public/name)
    copy_file(source/'prototype/trace/seccomp/filecache-node.json', public/'filecache-node.json')
    licences = engine/'licences'; licences.mkdir(mode=0o755)
    for component in ('launcher', 'worker'):
        for architecture in ('amd64', 'arm64'):
            name = component+'-'+architecture
            copy_public_tree(launchers/('licences-'+name), licences/name)
    (licences/'go').mkdir(mode=0o755)
    for name in ('LICENSE', 'PATENTS'):
        with (licences/'go'/name).open('xb') as output_file:
            subprocess.run(['docker', 'run', '--rm', '--network=none', '--cap-drop=ALL',
                            '--security-opt=no-new-privileges', '--read-only', '--memory=64m', '--cpus=1',
                            '--pids-limit=32', '--entrypoint=cat', worker['builder'], '/usr/local/go/'+name],
                           stdout=output_file, check=True, timeout=30)
        (licences/'go'/name).chmod(0o444)
    sources = engine/'sources'; sources.mkdir(mode=0o755)
    pack_tree(source/'prototype/trace/worker/.sdk', sources/'inspektor-gadget-v0.56.0-modified.tar', epoch,
              maximum=64 << 20, members=4000)
    (sources/'inspektor-gadget-v0.56.0-modified.tar').chmod(0o444)
    for name in ('build_worker.py', 'prepare_sdk.py', 'sdk_download.py', 'worker_dependencies.py', 'sdk-policy.patch'):
        copy_file(source/'prototype/trace/worker'/name, sources/name)
    copy_file(source/'prototype/trace/programmes/build_filecache.py', sources/'build_filecache.py')
    copy_public_tree(upstream/'include', sources/'upstream-headers')
    pack_tree(engine, output/'trace-engine.tar', epoch)
    pack_tree(context/'programmes', output/'trace-programmes.tar', epoch, maximum=128 << 20, members=256)
    # Public files only; a signing key or acceptance policy is never an input root.
    files = {str(path.relative_to(context)): sha(path) for path in sorted(context.rglob('*')) if path.is_file()}
    (output/'context-files.json').write_bytes(canonical(files))
    print('Public engine/programme archives assembled; downstream verification remains required.')


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ('source', 'workers', 'launchers', 'programmes', 'reproduced-programmes', 'verification', 'upstream', 'output'):
        parser.add_argument('--'+name, type=Path, required=True)
    parser.add_argument('--epoch', type=int, required=True)
    args = parser.parse_args()
    assemble(*(getattr(args, name).resolve() for name in ('source', 'workers', 'launchers', 'programmes',
               'reproduced_programmes', 'verification', 'upstream', 'output')), args.epoch)
