"""Explicit EKS endpoint/CA binding for the existing bounded isolation probes."""
import base64
import hashlib
import re
import ssl
import urllib.parse

from transport import QualificationError, Transport, fixture_name


def endpoint(value):
    try:
        parsed = urllib.parse.urlsplit(value)
        host, port = parsed.hostname, parsed.port or 443
        valid = (parsed.scheme == 'https' and parsed.username is None and parsed.password is None
                 and not parsed.path and not parsed.query and not parsed.fragment and port == 443
                 and not any(c in value for c in "?#")
                 and len(value) <= 512 and host and len(host) <= 253
                 and all(fixture_name(part) for part in host.split("."))
                 and host.endswith(('.eks.amazonaws.com', '.api.aws')))
    except (TypeError, ValueError):
        valid = False
    if not valid:
        raise QualificationError('exact EKS HTTPS endpoint required')
    return host, port


class EKSTransport(Transport):
    def __init__(self, binding, config):
        if (not isinstance(binding, dict) or set(binding) != {'endpoint', 'caSHA256'} or
                not isinstance(binding['caSHA256'], str) or
                not re.fullmatch('[a-f0-9]{64}', binding['caSHA256'])):
            raise QualificationError('exact EKS endpoint and CA binding required')
        address = endpoint(binding['endpoint'])
        try:
            if not all(len(config[key]) == 1 for key in ('users', 'clusters', 'contexts')):
                raise ValueError('ambiguous fixture context')
            context = config['contexts'][0]
            if (context['name'] != config['current-context'] or context['name'] != 'qualification-eks'
                    or set(context['context']) - {'cluster', 'user', 'namespace'}
                    or context['context']['cluster'] != config['clusters'][0]['name']
                    or context['context']['user'] != config['users'][0]['name']):
                raise ValueError('fixture context differs')
            cluster, user = config['clusters'][0]['cluster'], config['users'][0]['user']
            if set(cluster) != {'server', 'certificate-authority-data'} or set(user) != {'token'}:
                raise ValueError('alternate identity or TLS mechanism')
            ca = base64.b64decode(cluster['certificate-authority-data'], validate=True)
            if cluster['server'] != binding['endpoint'] or hashlib.sha256(ca).hexdigest() != binding['caSHA256']:
                raise ValueError('fixture endpoint or CA differs')
            tls = ssl.create_default_context(cadata=ca.decode())
        except (KeyError, TypeError, ValueError, UnicodeError, ssl.SSLError):
            raise QualificationError('pinned EKS fixture credentials required') from None
        self._configure(address, tls, user['token'])
