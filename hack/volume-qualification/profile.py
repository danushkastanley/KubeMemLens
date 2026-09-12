"""Frozen budgets describe this small local reference, not provider scale."""
from common import exact, require, integer, number, sha, digest, privacy

BUDGETS = 'collectorHeapPeakBytes collectorHeapIncreaseBytes collectorCPUIncreaseMilli producerCPUMilli producerMemoryBytes kubeletCPUIncreaseMilli kubeletMemoryIncreaseBytes sourceReadP95Seconds deliveryP95Seconds podReadP95Seconds workloadReadP95Seconds apiReadRate apiWriteRateIncrease usageCommitsPerMinute healthPayloadWrites usageRetainedBytes healthRetainedBytes responseBytes recoverySeconds'.split()


def validate(p):
    privacy(p)
    exact(p, 'schemaVersion id scope nodeImage kubernetes driverSource driverArchive registrarImage measurement budgets profileDigest', 'profile')
    require(type(p['schemaVersion']) is int and p['schemaVersion'] == 1 and p['id'] == 'kind-csi-137' and p['scope'] == 'local-reference', 'unsupported volume profile')
    require(p['kubernetes'] == 'v1.37.0', 'unsupported local Kubernetes version')
    require(p['nodeImage'] == 'kindest/node:v1.37.0@sha256:a1ed56cfb0e7b93589bdf97c8cd566405a265939e3620fc4f5de89adff580ae5', 'unpinned Node image')
    require(p['driverSource'] == 'eccd681b18a2c96332f33c2cac5db38656edd500', 'unexpected driver source')
    require(p['driverArchive'] == 'sha256:3b9e40a1f49435d1b264a28bc821580647d51fdec69d514e61bb70ec83f72bd8', 'unexpected driver archive')
    require(p['registrarImage'] == 'registry.k8s.io/sig-storage/csi-node-driver-registrar:v2.17.0@sha256:f9de845b170155199f2a2a3f9531cf13d78e31235e9db6b6582a8b0db0a50dad', 'unpinned registrar')
    m = p['measurement']
    exact(m, 'baselineSeconds enabledSeconds intervalSeconds settleSeconds', 'measurement')
    require(integer(m['baselineSeconds'], 120, 600) and integer(m['enabledSeconds'], 300, 1200), 'insufficient measurement window')
    require(integer(m['intervalSeconds'], 5, 30) and integer(m['settleSeconds'], 30, 120), 'invalid cadence')
    require(all(m[k] % m['intervalSeconds'] == 0 and m[k] // m['intervalSeconds'] <= 128 for k in ('baselineSeconds', 'enabledSeconds')), 'invalid sample count')
    exact(p['budgets'], ' '.join(BUDGETS), 'budgets')
    require(all(number(v) for v in p['budgets'].values()), 'invalid budget')
    require(p['budgets']['healthPayloadWrites'] == 0, 'unchanged health must not rewrite payloads')
    require(sha(p['profileDigest']) and digest({k:v for k,v in p.items() if k != 'profileDigest'}) == p['profileDigest'], 'profile digest mismatch')
    return p
