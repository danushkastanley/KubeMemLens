"""Bound all operations to one explicit Kubernetes context and private process streams."""

import json

from common import require
from process import execute


class KubernetesCommands:
    def __init__(self, kubeconfig, context, run=execute):
        require(kubeconfig and context, "explicit Kubernetes target is required")
        self.command = ["kubectl", "--kubeconfig", kubeconfig, "--context", context, "--request-timeout=10s"]
        self.run = run

    def __call__(self, *args, data=None, timeout=12, maximum=2 * 1024 * 1024):
        return self.run(self.command + list(args), data=data, timeout=timeout, maximum=maximum)

    def allowed(self, subject, verb, resource, subresource="", namespace="", name=""):
        identity = subject.split(":")
        require(len(identity) == 4 and identity[:2] == ["system", "serviceaccount"] and identity[2] and identity[3],
                "an explicit ServiceAccount identity is required")
        attributes = {"group": "", "verb": verb, "resource": resource, "subresource": subresource,
                      "namespace": namespace, "name": name}
        review = {"apiVersion": "authorization.k8s.io/v1", "kind": "SelfSubjectAccessReview",
                  "spec": {"resourceAttributes": attributes}}
        raw = self("--as", subject, "--as-group", "system:serviceaccounts", "--as-group", "system:serviceaccounts:" + identity[2],
                   "--as-group", "system:authenticated", "create", "--raw", "/apis/authorization.k8s.io/v1/selfsubjectaccessreviews",
                   "-f", "-", data=json.dumps(review).encode())
        status = json.loads(raw).get("status", {})
        require(type(status.get("allowed")) is bool and not status.get("evaluationError"), "authorisation review was inconclusive")
        return status["allowed"]
