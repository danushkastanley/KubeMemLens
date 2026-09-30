import copy
import unittest

from density import mapped_fixtures


class DensityTests(unittest.TestCase):
    def test_count_cannot_mask_unmapped_replaced_or_stale_fixture(self):
        bound = {'podUID': 'uid', 'container': 'cid', 'identity': 'identity', 'group': {'path': '/sys/fs/cgroup/pod/cid'}}
        row = {'namespace': 'fixture', 'podName': 'target', 'podUID': 'uid', 'containerID': 'cid',
               'containerName': 'worker', 'nodeName': 'node', 'freshness': 'fresh', 'completeness': 'complete', 'cgroupPath': 'pod/cid'}
        document = {'kind': 'ContainerMemoryList', 'metadata': {}, 'items': [{'snapshot': row}]}
        bindings = {'fixture/target': bound}
        self.assertEqual(mapped_fixtures(document, 'fixture', bindings, 'node')['mappedContainers'], 1)
        for key, value in [('podUID', 'replaced'), ('podName', 'unrelated'), ('freshness', 'stale'),
                           ('completeness', 'partial'), ('cgroupPath', 'other')]:
            changed = copy.deepcopy(document)
            changed['items'][0]['snapshot'][key] = value
            with self.subTest(key=key), self.assertRaises(ValueError):
                mapped_fixtures(changed, 'fixture', bindings, 'node')
        for items in [[], [document['items'][0]] * 2]:
            with self.assertRaises(ValueError):
                mapped_fixtures({**document, 'items': items}, 'fixture', bindings, 'node')
        with self.assertRaises(ValueError):
            mapped_fixtures({**document, 'metadata': {'continue': 'next'}}, 'fixture', bindings, 'node')


if __name__ == '__main__':
    unittest.main()
