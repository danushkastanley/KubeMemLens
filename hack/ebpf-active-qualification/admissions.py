"""Retain every known owned admission until cancellation is confirmed."""
import re

from local_case import TraceClient


class Admissions:
    def __init__(self, runtime, namespaces):
        if not 1 <= len(namespaces) <= 2 or len(set(namespaces)) != len(namespaces):
            raise ValueError('one or two distinct owned admission namespaces required')
        self.clients = [TraceClient(runtime, ns) for ns in namespaces]
        self.engine = 'sha256:' + runtime.cfg['engineSHA256']
        self.pending = {}

    def __repr__(self):
        return '<owned local admissions>'

    def client(self, index):
        if type(index) is not int or not 0 <= index < len(self.clients):
            raise ValueError('admission target outside owned inventory')
        return self.clients[index]

    def create(self, index, intent):
        client = self.client(index)
        if index in self.pending:
            raise ValueError('previous admission still requires cancellation')
        status, admission = client.call('POST', value=intent)
        if status != 201 or not isinstance(admission, dict):
            raise ValueError('admission was not created')
        return self.retain(index, admission)

    def retain(self, index, admission):
        self.client(index)
        if index in self.pending:
            raise ValueError('previous admission still requires cancellation')
        metadata = admission.get('metadata')
        name = metadata.get('name') if isinstance(metadata, dict) else None
        if not isinstance(name, str) or re.fullmatch(r'[a-zA-Z0-9-]{1,128}', name) is None:
            raise ValueError('created admission has no bounded identity; source teardown required')
        # Retain identity before checking other response fields, so a mismatched
        # response cannot hide a created resource from cleanup.
        self.pending[index] = '/' + name
        if admission.get('engineDigest') != self.engine:
            raise ValueError('admission did not match candidate')
        return admission

    def get(self, index):
        return self.client(index).call('GET', self.pending[index])

    def cancel(self, index):
        client = self.client(index)
        path = self.pending[index]
        status, _ = client.call('DELETE', path)
        if status not in (200, 404, 410):
            raise ValueError('owned admission cancellation unconfirmed')
        del self.pending[index]

    def cancel_all(self):
        failures = []
        for index in tuple(self.pending):
            try:
                self.cancel(index)
            except Exception:
                failures.append(index)
        if failures:
            raise ValueError('owned admission cleanup incomplete; source teardown required')
