"""Replay each concurrent receiver and its target binding without pooling verdicts."""
from local_case import digest
from processes import read_document
from samples import integer, require


def concurrent_session_budgets(directory, control, enabled):
    bindings = enabled.get('targetBindings')
    require(type(bindings) is list and len(bindings) == 2 and bindings == control.get('targetBindings'),
            'paired concurrent target identities changed or are missing')
    for index, binding in enumerate(bindings):
        integer(binding['targetIndex'], 0, 1)
        integer(binding['witnessOrdinal'], 0, 1)
        require(binding['targetIndex'] == index, 'target binding order changed')
    require({b['witnessOrdinal'] for b in bindings} == {0, 1}, 'duplicate target witness ordinal')
    identities = {b['identity'] for b in bindings}
    fixtures = {value for key, value in enabled['fixtureIdentities'].items() if key.endswith('/target')}
    require(len(identities) == 2 and identities == fixtures, 'receiver bindings differ from fixture targets')
    delivery_pass, deadline_pass = True, True
    for session in enabled['sessions']:
        probe = session.get('capacityProbe', {})
        require(probe.get('thirdRequestCapacityDenied') is True
                and probe.get('bothAdmissionsActiveBeforeAndAfter') is True,
                'concurrent capacity probe incomplete')
        targets = session.get('targets')
        require(type(targets) is list and len(targets) == 2, 'both concurrent receiver results required')
        for index, target in enumerate(targets):
            integer(target['targetIndex'], 0, 1)
            require(target['targetIndex'] == index, 'concurrent receiver order changed')
            path = directory / f"delivery-{session['index']:02}-target-{index}.jsonl"
            raw = read_document(path)
            require(digest(path.read_bytes()) == target['deliverySHA256'] and raw['latency'] == target['latency'],
                    'concurrent delivery differs from its retained stream')
            require(raw.get('sameKernelClock') is True and raw['observation']['transportComplete'] is True
                    and raw['observation']['hookCoverageIncomplete'] is True,
                    'concurrent receiver clock or transport incomplete')
            integer(target['latency']['receivedEvents'], 1)
            integer(target['deadlineTeardownUpperNanos'])
            integer(target['attachUpperNanos'])
            for field in ('normalLossBudgetPassed', 'eventDeliveryBudgetPassed'):
                require(type(target['latency'][field]) is bool, 'missing receiver budget verdict')
                delivery_pass = delivery_pass and target['latency'][field]
            deadline_pass = deadline_pass and session.get('zeroOwnedState') is True and target['deadlineTeardownUpperNanos'] < 2000000000
    return delivery_pass, deadline_pass
