"""Synthetic owned scheduler records for replay tests only; never runtime evidence."""
import json

from local_case import canonical, digest
from test_scheduler_window import samples

BOOT = '11111111-1111-1111-1111-111111111111'


def owner_binding():
    value = {'pid': 42, 'start': 100, 'container': 'a' * 64, 'sha256': 'b' * 64,
             'podName': 'owned-collector', 'podUID': 'owned-collector-uid', 'startedAt': '2026-10-01T00:00:00Z',
             'group': {'role': 'collector', 'path': '/sys/fs/cgroup/owned-collector', 'inode': 123},
             'specSHA256': 'c' * 64, 'controllerUID': 'owned-deployment-uid',
             'controllerSpecSHA256': 'e' * 64}
    value['identity'] = digest(canonical(value))
    return value


def write_scheduler(directory, seconds, receipt, boot=BOOT):
    owner = owner_binding()
    receipt['schedulerOwner'] = owner
    receipt['standardIdentities']['collector'] = owner['identity']
    config = {'seconds': seconds, 'bootID': boot, 'onlineCPUs': '0-1',
              'anchor': {k: owner[k] for k in ('pid', 'start')},
              'tracepointSHA256': {name: 'd' * 64 for name in
                  ('sched_wakeup', 'sched_wakeup_new', 'sched_switch', 'sched_process_exit')}}
    (directory / 'scheduler-input.private.json').write_bytes(canonical(config))
    (directory / 'scheduler.jsonl').write_text(''.join(json.dumps(row) + '\n' for row in samples(seconds)))
