"""Private controller input and certificate lifetime checks before local changes."""
import base64
import json
import os
import stat

from local_case import command, digest

FIELDS = {'trace', 'sourceSHA256', 'fixtureNamespaces', 'standardNamespace', 'release',
          'standardImage', 'fixtureImage', 'fixtureSHA256', 'standardSHA256', 'helpers', 'owner', 'chartInventory'}


def unique_fields(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise ValueError('duplicate configuration field')
        result[key] = value
    return result


def read_configuration(path):
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    with os.fdopen(fd, 'rb') as stream:
        info = os.fstat(stream.fileno())
        if not stat.S_ISREG(info.st_mode) or info.st_mode & 0o077 or info.st_uid != os.getuid():
            raise ValueError('configuration must be a private owned regular file')
        raw = stream.read(32769)
    if len(raw) > 32768:
        raise ValueError('configuration exceeds bound')
    cfg = json.loads(raw, object_pairs_hook=unique_fields,
                     parse_constant=lambda _: (_ for _ in ()).throw(ValueError('invalid JSON constant')))
    if not isinstance(cfg, dict) or set(cfg) != FIELDS:
        raise ValueError('configuration fields differ from campaign contract')
    return cfg


def certificate_lifetimes(runtime, profile):
    # Includes both windows, warmups and ten minutes of bounded setup/cleanup per
    # pair, plus a final restoration margin. Check certificates, never private keys.
    minimum = profile['pairs'] * (2 * (profile['windowSeconds'] + profile['warmupSeconds']) + 600) + 600
    receipts = []
    for role in ('node', 'api'):
        volumes = runtime.deployment(role)['spec']['template']['spec']['volumes']
        tls = [v['secret']['secretName'] for v in volumes if v['name'] == 'tls' and 'secret' in v]
        if len(tls) != 1:
            raise ValueError('unique mounted TLS secret required')
        secret = runtime.get('secret', tls[0])
        certificates = {k: v for k, v in secret['data'].items() if k.endswith('.crt')}
        if len(certificates) != (2 if role == 'node' else 3):
            raise ValueError('TLS certificate inventory changed')
        for name, encoded in sorted(certificates.items()):
            pem = base64.b64decode(encoded, validate=True)
            if pem.count(b'-----BEGIN CERTIFICATE-----') != 1:
                raise ValueError('single bound certificate required')
            command(['openssl', 'x509', '-noout', '-checkend', str(minimum)], pem)
            receipts.append({'role': role, 'certificate': name, 'sha256': digest(pem),
                             'remainingSecondsAtLeast': minimum})
    return receipts
