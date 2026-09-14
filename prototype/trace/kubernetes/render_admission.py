"""Render the administrator-installed optional admission profile; never deploy it."""
import argparse
import base64
import hashlib
import json
import re
import ssl
from pathlib import Path

from admission_resources import deployments, permissions, resource


def render(image, node, uid, namespace, kubelet_root, certificate_directory):
    if not re.fullmatch(r"[a-zA-Z0-9./:_-]+@sha256:[a-f0-9]{64}", image):
        raise ValueError("an immutable image digest is required")
    if not re.fullmatch(r"[a-z0-9](?:[a-z0-9.-]{0,251}[a-z0-9])?", node):
        raise ValueError("invalid Node name")
    if not re.fullmatch(r"[a-z0-9-]{1,128}", uid):
        raise ValueError("invalid Node UID")
    if not re.fullmatch(r"[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?", namespace):
        raise ValueError("invalid administrator namespace")
    if not re.fullmatch(r"/(?:[a-z0-9]+(?:-[a-z0-9]+)*(?:/[a-z0-9]+(?:-[a-z0-9]+)*){0,3})?", kubelet_root):
        raise ValueError("invalid kubelet cgroup root")
    if any(len(part) > 63 for part in kubelet_root.split("/")):
        raise ValueError("kubelet cgroup component exceeds 63 bytes")
    ns = namespace
    directory = Path(certificate_directory)
    def read(name):
        with (directory / name).open("rb") as stream:
            data = stream.read(65537)
        if not data or len(data) > 65536:
            raise ValueError("invalid certificate file size")
        return data.decode("ascii")
    ca_pem = read("ca.crt").encode("ascii")
    ncert, nkey = read("node.crt"), read("node.key")
    ccert, ckey = read("control.crt"), read("control.key")
    acert, akey = read("api.crt"), read("api.key")
    npin = hashlib.sha256(ssl.PEM_cert_to_DER_cert(ncert)).hexdigest()
    cpin = hashlib.sha256(ssl.PEM_cert_to_DER_cert(ccert)).hexdigest()
    node_dns = f"binding-node.{ns}.svc"
    items = []
    for name, data in (
        ("binding-node-tls", {"tls.crt": ncert, "tls.key": nkey, "control-ca.crt": ca_pem.decode()}),
        ("admission-api-tls", {"tls.crt": acert, "tls.key": akey, "node-client.crt": ccert,
                               "node-client.key": ckey, "node-ca.crt": ca_pem.decode()}),
    ):
        items.append(resource("Secret", name, ns, type="Opaque", stringData=data))
    registry = [{"nodeUID": uid, "nodeName": node, "url": "https://" + node_dns + ":9443",
                 "caFile": "/tls/node-ca.crt", "certificateSHA256": npin}]
    items.append(resource("ConfigMap", "node-registry", ns, data={"nodes.json": json.dumps(registry)}))
    items += permissions(ns)
    items += deployments(image, node, uid, ns, kubelet_root, cpin)
    items.append(resource("APIService", "v1alpha1.tracing.kubememlens.io", None,
                          "apiregistration.k8s.io/v1", spec={
        "group": "tracing.kubememlens.io", "version": "v1alpha1",
        "groupPriorityMinimum": 1000, "versionPriority": 15,
        "service": {"namespace": ns, "name": "admission-api", "port": 8443},
        "caBundle": base64.b64encode(ca_pem).decode(), "insecureSkipTLSVerify": False,
    }))
    return {"apiVersion": "v1", "kind": "List", "items": items}


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ("image", "node", "node-uid", "namespace", "kubelet-cgroup-root", "certificate-directory"):
        parser.add_argument("--" + name, required=True)
    args = parser.parse_args()
    try:
        result = render(args.image, args.node, args.node_uid, args.namespace,
                        args.kubelet_cgroup_root, args.certificate_directory)
    except (ValueError, OSError) as error:
        parser.error(str(error))
    print(json.dumps(result, indent=2))
