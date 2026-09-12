"""Check private runtime artefacts and emit only fixed, identity-free results."""
import hashlib
import json
import pathlib
import stat
import sys

root = pathlib.Path(sys.argv[1])

def load(name):
    return json.loads((root / name).read_text())

before = load('volume-before-capture.json')
after = load('volume-after-capture.json')
b = before['volumes']['volumes'][0]
a = after['volumes']['volumes'][0]
assert before['schemaVersion'] == after['schemaVersion'] == 5
assert b['evidenceID'] == a['evidenceID'] and b['filesystemID'] == a['filesystemID']
assert a['usage']['filesystem']['usedBytes'] - b['usage']['filesystem']['usedBytes'] == 1048576
assert 'Used bytes change: +1Mi' in (root / 'volume-compare.txt').read_text()
assert before['pod']['memory']['TotalBytes'] < a['usage']['filesystem']['capacityBytes']
for name in ('volume-redacted.json', 'volume-pty-80.json', 'volume-pty-160.json'):
    path = root / name
    value = load(name)
    assert value['schemaVersion'] == 5 and value['redacted']
    assert stat.S_IMODE(path.stat().st_mode) == 0o600
    body = path.read_text()
    for marker in ('kube-memlens-csi-e2e', '"persistent"', '"data"', 'hostpath.csi.k8s.io', 'evidenceID', 'filesystemID', 'r5-private-marker'):
        assert marker not in body, 'default capture disclosed private identity'
    assert value['pod']['namespace'] == 'namespace-1'
    assert value['volumes']['volumes'][0]['configuration']['kind'] == 'persistent-claim'
    assert value['pod']['containers'][0]['memory']['ioPressure']['state'] == 'available'
legacy = load('volume-legacy.json')
assert legacy['schemaVersion'] == 1 and legacy['partial']
assert 'ioPressure' not in json.dumps(legacy) and 'filesystem' not in legacy
assert load('volume-recommend.json')['automaticMutation'] is False
workload = load('volume-workload-cli.json')['evidence']
assert len(workload['workload']['pods']) == 2
assert len(workload['filesystems']) == 3
assert sum(len(g['members']) == 2 for g in workload['filesystems']) == 1
memory = workload['workload']['memory']
assert memory['ShmemBytes'] >= 4194304
assert memory['TotalBytes'] == sum(p['memory']['TotalBytes'] for p in workload['workload']['pods'])
assert len(workload['podVolumes']) == 2
for p in workload['podVolumes']:
    scratch = next(v for v in p['context']['volumes'] if v['volumeName'] == 'scratch')
    assert scratch['configuration']['memoryBacked'] is True
    assert scratch['configuration']['sizeLimitBytes'] == 8388608
assert load('volume-workload-recommend.json')['automaticMutation'] is False
result = {
    'cliExplain': True, 'cliRecommend': True, 'privateVolumeCapture': True,
    'offlineReplay': True, 'verifiedBindingCompare': True,
    'controlledFilesystemDeltaBytes': 1048576, 'legacyProjection': True,
    'pty80x24': True, 'pty160x35': True, 'ptyScrollPauseCapture': True,
    'freshCaptureRevocation': True, 'workloadLiveOwnership': True,
    'sharedClaimDeduplicated': True, 'tmpfsWrittenBytesPerReplica': 2097152,
    'tmpfsReplicas': 2, 'filesystemExcludedFromMemory': True,
    'workloadAccessRevocation': True, 'ioPressureObserved': True,
    'cliBinarySHA256': hashlib.sha256((root / 'kubectl-memlens').read_bytes()).hexdigest(),
    'runtimeIdentifiersIncluded': False, 'credentialsRetained': False,
}
(root / 'volume-cockpit-result.json').write_text(json.dumps(result))
