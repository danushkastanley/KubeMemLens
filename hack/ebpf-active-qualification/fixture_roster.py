"""Deterministic fixture identities keep ten noisy peers outside admission targets."""
from noisy_profile import CASE
from nonselected_workload import ROLES
from concurrent_profile import CASE as CONCURRENT_CASE


def noise_targets(profile, namespaces):
    if profile['case'] != CASE:
        return []
    if len(namespaces) != 2 or namespaces[0] == namespaces[1]:
        raise ValueError('two distinct workload namespaces required')
    return [{'namespace': namespaces[index // 5], 'name': 'noise-' + str(index), 'role': role}
            for index, role in enumerate(ROLES)]


def fixture_roster(profile, namespaces):
    roster = [{'namespace': ns, 'name': 'target', 'role': 'selected'} for ns in namespaces]
    if profile['case'] == CONCURRENT_CASE:
        roster[1]['role'] = 'selected-peer'
    roster += noise_targets(profile, namespaces)
    roster += [{'namespace': namespaces[0], 'name': f'passive-{index:02}', 'role': 'selected'}
               for index in range(profile['workloadContainers'] - len(roster))]
    if len(roster) != profile['workloadContainers']:
        raise ValueError('fixture density cannot contain the required roster')
    return roster
