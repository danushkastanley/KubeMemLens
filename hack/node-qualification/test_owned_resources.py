import json
import tempfile
import unittest
from pathlib import Path

from common import ContractError, load
from owned_resources import OwnedResources, Resource


class Kubernetes:
    def __init__(self):
        self.objects, self.deletes, self.counter = {}, [], 0
        self.read_error = False

    def __call__(self, *args, data=None):
        if args[0] == "get":
            if self.read_error:
                raise ContractError("command failed")
            namespace = args[args.index("--namespace") + 1] if "--namespace" in args else ""
            matches = [d for r, d in self.objects.items() if r.kind == args[1] and r.name == args[2] and r.namespace == namespace]
            return json.dumps(matches[0]) if matches else ""
        if args[0] == "create":
            value = json.loads(data); resource = Resource.from_object(value)
            if resource in self.objects:
                raise ContractError("already exists")
            self.counter += 1
            value["metadata"]["uid"] = "uid-" + str(self.counter)
            self.objects[resource] = value
            return json.dumps(value)
        if args[0] == "delete":
            options = json.loads(data)
            self.deletes.append((args[2], options))
            resource = next(r for r in self.objects if r.uri == args[2])
            if self.objects[resource]["metadata"]["uid"] != options["preconditions"]["uid"]:
                raise ContractError("UID conflict")
            del self.objects[resource]
            return "{}"
        raise AssertionError(args)


class OwnershipTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.k = Kubernetes()
        self.owned = OwnedResources(self.k, Path(self.temp.name) / "journal")
        self.namespace = {"apiVersion": "v1", "kind": "Namespace", "metadata": {"name": "fixture"}}
        self.secret = {"apiVersion": "v1", "kind": "Secret", "metadata": {"name": "secret", "namespace": "fixture"},
                       "data": {"private-key": "private-payload"}}

    def test_cleanup_uses_original_uid_and_journal_omits_payload(self):
        self.owned.create(self.namespace)
        self.owned.create(self.secret)
        journal = load(self.owned.journal / "1.json")
        self.assertNotIn("private-payload", json.dumps(journal))
        self.assertEqual(journal["uid"], "uid-2")
        self.owned.cleanup()
        self.assertFalse(self.k.objects)
        self.assertEqual([options["preconditions"]["uid"] for _, options in self.k.deletes], ["uid-2", "uid-1"])

    def test_replaced_child_and_parent_namespace_are_preserved(self):
        self.owned.create(self.namespace)
        self.owned.create(self.secret)
        resource = Resource.from_object(self.secret)
        self.k.objects[resource]["metadata"]["uid"] = "someone-elses-replacement"
        with self.assertRaises(ContractError):
            self.owned.cleanup()
        self.assertIn(resource, self.k.objects)
        self.assertIn(Resource.from_object(self.namespace), self.k.objects)
        self.assertFalse(self.k.deletes)

    def test_create_response_loss_retains_unknown_child_and_parent_for_inspection(self):
        self.owned.create(self.namespace)
        def response_lost(*args, data=None):
            result = self.k(*args, data=data)
            if args[0] == "create":
                raise ContractError("response lost after creation")
            return result
        self.owned.k = response_lost
        with self.assertRaises(ContractError):
            self.owned.create(self.secret)
        with self.assertRaises(ContractError):
            self.owned.cleanup()
        self.assertIn(Resource.from_object(self.namespace), self.k.objects)
        self.assertIn(Resource.from_object(self.secret), self.k.objects)
        self.assertFalse(self.k.deletes)
        self.assertNotIn("private-payload", "".join(p.read_text() for p in self.owned.journal.glob("*.json")))

    def test_existing_resource_is_never_adopted(self):
        self.k("create", data=json.dumps(self.namespace).encode())
        with self.assertRaises(ContractError):
            self.owned.require_absent([Resource.from_object(self.namespace)])
        with self.assertRaises(ContractError):
            self.owned.create(self.namespace)
        self.assertFalse(self.owned.owned)

    def test_read_failure_is_not_absence(self):
        self.k.read_error = True
        with self.assertRaises(ContractError):
            self.owned.require_absent([Resource.from_object(self.namespace)])

    def test_namespace_replacement_is_not_deleted(self):
        self.owned.create(self.namespace)
        resource = Resource.from_object(self.namespace)
        self.k.objects[resource]["metadata"]["uid"] = "replacement"
        with self.assertRaises(ContractError):
            self.owned.cleanup()
        self.assertFalse(self.k.deletes)

    def test_resource_paths_reject_escape_and_scope_changes(self):
        for args in (("v1", "Namespace", "../other"), ("v1", "Secret", "name"),
                     ("rbac.authorization.k8s.io/v1", "ClusterRole", "name", "namespace")):
            with self.assertRaises(ContractError):
                Resource(*args)


if __name__ == "__main__":
    unittest.main()
