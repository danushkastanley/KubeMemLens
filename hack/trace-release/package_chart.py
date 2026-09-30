"""Prepare a disabled trace chart pinned to one verified image index."""
import argparse
import importlib.util
from pathlib import Path
import re

from manifest import IMAGE_REPOSITORY, VERSION, match, require
from pack import copy_public_tree


def prepare(source, output, version, image_digest, epoch):
    require(match(VERSION, version) and len(version) <= 64 and
            re.fullmatch('sha256:[a-f0-9]{64}', image_digest) and
            type(epoch) is int and 0 <= epoch <= 2**32-1, 'invalid chart build identity')
    output.mkdir(mode=0o700)
    chart = output/'kube-memlens-trace'
    copy_public_tree(source, chart)
    metadata = chart/'Chart.yaml'
    raw = metadata.read_text()
    for field in ('version', 'appVersion'):
        raw, count = re.subn(r'(?m)^'+field+r':[^\n]*$', field+': "'+version+'"', raw)
        require(count == 1, 'chart version metadata is ambiguous')
    metadata.chmod(0o644)
    metadata.write_text(raw)
    values = chart/'values.yaml'
    raw = values.read_text()
    for old, replacement in [('  repository: ""', '  repository: "'+IMAGE_REPOSITORY+'"'),
                              ('  digest: ""', '  digest: "'+image_digest+'"')]:
        require(raw.count(old) == 1, 'chart image defaults are ambiguous')
        raw = raw.replace(old, replacement)
    values.chmod(0o644)
    values.write_text(raw)
    # Reuse the standard product's deterministic tar/gzip producer. This loads
    # trusted repository tooling; candidate chart contents are never executable.
    tool = Path(__file__).resolve().parent.parent/'release/package_chart.py'
    spec = importlib.util.spec_from_file_location('deterministic_chart', tool)
    packager = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(packager)
    archive = output/('kube-memlens-trace-'+version+'.tgz')
    packager.package_chart(chart, archive, epoch)
    print('Disabled image-pinned chart packaged; inspection and render checks remain required.')


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--source', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--version', required=True)
    parser.add_argument('--image-digest', required=True)
    parser.add_argument('--epoch', type=int, required=True)
    args = parser.parse_args()
    prepare(args.source.resolve(), args.output.resolve(), args.version, args.image_digest, args.epoch)
