"""Validate bounded observations without assigning provider support."""
import re
from common import exact, require, number, integer, instant, sha, digest, privacy
from profile import validate as validate_profile

NUMBERS = 'elapsedSeconds heapObjectsBytes collectorCPUMilli producerCPUMilli producerMemoryBytes kubeletCPUMilli kubeletMemoryBytes sourceReads sourceErrors sourceReadSeconds sourceResponseBytes postSuccess postFailures postSeconds usageCommits usageBytes healthWrites healthBytes apiReads apiWrites'.split()
OPTIONAL = 'volumeReads volumeErrors podSeconds workloadSeconds'.split()
WORKFLOW = 'doctor cliExplain cliRecommend privateVolumeCapture offlineReplay verifiedBindingCompare legacyProjection pty80x24 pty160x35 freshCaptureRevocation workloadLiveOwnership sharedClaimDeduplicated filesystemExcludedFromMemory workloadAccessRevocation ioPressureObserved filesystemPressure inodePressure healthConflict sourceRecovery namespaceIsolation rollback'.split()


def validate_window(p, w, phase):
    exact(w, 'startedAt completedAt stable samples', 'window')
    start, end = instant(w['startedAt']), instant(w['completedAt'])
    m = p['measurement']; seconds = m[phase + 'Seconds']; interval = m['intervalSeconds']
    require(end > start and seconds <= (end-start).total_seconds() <= seconds + 2*interval, 'window time coverage incomplete')
    require(type(w['stable']) is bool and type(w['samples']) is list and len(w['samples']) == seconds//interval + 1, 'window sample count mismatch')
    prior = None
    for index, row in enumerate(w['samples']):
        exact(row, ' '.join(NUMBERS + OPTIONAL + ['filesystemAt', 'usageEnabled', 'healthEnabled']), 'sample')
        require(all(number(row[k]) for k in NUMBERS), 'missing or invalid measurement')
        require(all(type(row[k]) is bool and row[k] == (phase == 'enabled') for k in ('usageEnabled', 'healthEnabled')), 'measurement mode mismatch')
        if phase == 'baseline':
            require(all(row[k] is None for k in OPTIONAL + ['filesystemAt']), 'baseline unexpectedly queried volume evidence')
        else:
            require(all(number(row[k]) for k in OPTIONAL), 'missing enabled measurement')
            at = instant(row['filesystemAt'])
            require(-30 <= (start-at).total_seconds() + row['elapsedSeconds'] <= 45, 'stale or future filesystem sample')
        elapsed = row['elapsedSeconds']
        require(index*interval <= elapsed <= index*interval + interval, 'sample outside declared cadence')
        require(prior is None or elapsed > prior, 'unordered samples')
        prior = elapsed
    return w


def validate(p, e):
    validate_profile(p); privacy(e)
    exact(e, 'schemaVersion scope profile source environment workflow windows recovery cleanup recordDigest', 'evidence')
    require(type(e['schemaVersion']) is int and e['schemaVersion'] == 1 and e['scope'] == 'local-reference', 'unsupported evidence scope')
    require(e['profile'] == {'id':p['id'], 'digest':p['profileDigest']}, 'wrong frozen profile')
    s=e['source']
    exact(s, 'commit tree dirty producer collector cli chart image driverImage driverBinary', 'source')
    require(isinstance(s['commit'],str) and re.fullmatch('[a-f0-9]{40}',s['commit']) and type(s['dirty']) is bool, 'invalid source identity')
    require(all(sha(s[k]) for k in ('tree','producer','collector','cli','chart','image','driverImage','driverBinary')), 'missing source/build digests')
    env=e['environment']
    exact(env, 'kubernetes nodeImage kernel runtime architecture driverSource registrarImage registrarRuntimeImage externalHealthMonitor controllerEvidence driverCapabilities', 'environment')
    require(all(env[k]==p[k] for k in ('kubernetes','nodeImage','driverSource','registrarImage')), 'environment differs from profile')
    require(env['architecture'] in ('amd64','arm64') and all(isinstance(env[k],str) and env[k] for k in ('kernel','runtime')), 'missing runtime inventory')
    require(env['externalHealthMonitor']=='not-installed' and env['controllerEvidence']=='api-fixture', 'false controller health provenance')
    require(sha(env['registrarRuntimeImage']), 'missing running registrar image identity')
    caps=env['driverCapabilities']
    require(type(caps) is list and 1<=len(caps)<=32 and all(isinstance(v,str) for v in caps), 'invalid driver capability inventory')
    require(len(set(caps))==len(caps), 'duplicate driver capabilities')
    require(all(isinstance(v,str) and re.fullmatch('[A-Z][A-Z_]{0,63}',v) for v in caps) and 'GET_VOLUME_STATS' in caps, 'filesystem capability not advertised')
    exact(e['workflow'],' '.join(WORKFLOW),'workflow')
    require(all(type(v) is bool for v in e['workflow'].values()), 'invalid workflow result')
    exact(e['windows'],'baseline enabled','windows')
    for phase in ('baseline','enabled'):validate_window(p,e['windows'][phase],phase)
    require(instant(e['windows']['baseline']['completedAt']) < instant(e['windows']['enabled']['startedAt']), 'overlapping measurement phases')
    exact(e['recovery'], 'collector producer source pod', 'recovery')
    require(all(number(v) and v > 0 for v in e['recovery'].values()), 'unmeasured recovery')
    require(e['cleanup'] in ('passed','failed','pending'), 'invalid cleanup state')
    require(sha(e['recordDigest']) and digest({k:v for k,v in e.items() if k!='recordDigest'})==e['recordDigest'], 'evidence digest mismatch')
    return e
