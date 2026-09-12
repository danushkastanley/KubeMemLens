"""Prepare serving TLS and local image aliases only inside an owned kind fixture."""

import argparse
import json
import re
from pathlib import Path

from common import require, write_new
from local_image_graph import inspect as inspect_image
from archive_index import from_archive
from process import execute


def transfer_public(source, source_path, target, target_path):
    require(source_path.endswith((".csr", ".crt")) and target_path.endswith((".csr", ".crt")), "only public certificate paths may be transferred")
    # Docker's archive API cannot reliably read kind's tmpfs mounts. Exec uses
    # the container's actual mount namespace and keeps transfer size bounded.
    data = execute(["docker", "exec", source, "cat", source_path], maximum=16 * 1024)
    require(data.startswith(("-----BEGIN CERTIFICATE REQUEST-----", "-----BEGIN CERTIFICATE-----"))
            and "PRIVATE KEY" not in data, "only public certificate data may be transferred")
    execute(["docker", "exec", "-i", target, "sh", "-c", 'umask 077; cat > "$1"', "sh", target_path], data=data.encode())


def prepare(args):
    root = Path(args.private)
    require(args.cluster.startswith("kube-memlens-node-context-"), "unexpected owned fixture name")
    nodes = execute(["kind", "get", "nodes", "--name", args.cluster]).splitlines()
    require(len(nodes) == 2, "two local fixture Nodes are required")
    for node in nodes:
        owner = execute(["docker", "inspect", "--format", '{{index .Config.Labels "io.x-k8s.kind.cluster"}}', node]).strip()
        require(owner == args.cluster, "refusing changes outside the owned fixture")
    control = [n for n in nodes if execute(["docker", "inspect", "--format", '{{index .Config.Labels "io.x-k8s.kind.role"}}', n]).strip() == "control-plane"]
    require(len(control) == 1, "owned control plane is ambiguous")
    control = control[0]
    k = ["kubectl", "--kubeconfig", args.kubeconfig, "--context", "kind-" + args.cluster, "--request-timeout=10s"]
    observed = json.loads(execute(k + ["get", "nodes", "-o", "json"]))["items"]
    manifests = set()
    image_proofs = []
    for index, node in enumerate(nodes):
        rows = execute(["docker", "exec", node, "ctr", "-n", "k8s.io", "images", "ls"]).splitlines()
        images = [row.split() for row in rows if row.split() and row.split()[0] == args.image]
        require(len(images) == 1 and re.fullmatch(r"sha256:[a-f0-9]{64}", images[0][2]), "local image manifest is unavailable")
        digest = images[0][2]; manifests.add(digest)
        alias = args.repository + "@" + digest
        if not any(row.split() and row.split()[0] == alias for row in rows):
            execute(["docker", "exec", node, "ctr", "-n", "k8s.io", "images", "tag", args.image, alias])
        record = next(n for n in observed if n["metadata"]["name"] == node)
        image_proofs.append(inspect_image(node, digest, record["status"]["nodeInfo"]["architecture"]))
        addresses = [a["address"] for a in record["status"]["addresses"] if a["type"] == "InternalIP" and ":" not in a["address"]]
        require(len(addresses) == 1, "fixture requires one IPv4 Node address")
        config = f"[req]\ndistinguished_name=dn\nprompt=no\n[dn]\nCN={node}\n[serving]\nbasicConstraints=CA:FALSE\nkeyUsage=digitalSignature,keyEncipherment\nextendedKeyUsage=serverAuth\nsubjectAltName=IP:{addresses[0]},DNS:{node}\n"
        execute(["docker", "exec", "-i", node, "sh", "-c", "umask 077; cat > /tmp/qualification-serving.cnf"], data=config.encode())
        execute(["docker", "exec", node, "openssl", "req", "-new", "-newkey", "rsa:2048", "-nodes", "-config", "/tmp/qualification-serving.cnf",
                 "-keyout", "/var/lib/kubelet/pki/qualification.key", "-out", "/tmp/qualification.csr"])
        transfer_public(node, "/tmp/qualification.csr", control, "/tmp/qualification-sign.csr")
        execute(["docker", "exec", "-i", control, "sh", "-c", "umask 077; cat > /tmp/qualification-sign.cnf"], data=config.encode())
        execute(["docker", "exec", control, "openssl", "x509", "-req", "-in", "/tmp/qualification-sign.csr", "-CA", "/etc/kubernetes/pki/ca.crt",
                 "-CAkey", "/etc/kubernetes/pki/ca.key", "-set_serial", str(1001 + index), "-days", "1", "-extfile", "/tmp/qualification-sign.cnf",
                 "-extensions", "serving", "-out", "/tmp/qualification-signed.crt"])
        transfer_public(control, "/tmp/qualification-signed.crt", node, "/var/lib/kubelet/pki/qualification.crt")
        execute(["docker", "exec", node, "sh", "-c", 'sed -i "/^tlsCertFile:/d; /^tlsPrivateKeyFile:/d" /var/lib/kubelet/config.yaml; printf "\\ntlsCertFile: /var/lib/kubelet/pki/qualification.crt\\ntlsPrivateKeyFile: /var/lib/kubelet/pki/qualification.key\\n" >> /var/lib/kubelet/config.yaml; systemctl restart kubelet'])
    require(len(manifests) == 1, "fixture Nodes loaded different image manifests")
    require(all(proof == image_proofs[0] for proof in image_proofs), "fixture Nodes resolved different image platforms")
    image_proofs[0]["archiveIndexDigest"] = from_archive(root / "image-archive.tar", next(iter(manifests)))
    write_new(root / "image-proof.json", image_proofs[0])
    (root / "image-digest").write_text(next(iter(manifests)))
    execute(["docker", "cp", control + ":/etc/kubernetes/pki/ca.crt", str(root / "serving-ca.crt")])
    execute(k + ["wait", "nodes", "--all", "--for=condition=Ready", "--timeout=90s"], timeout=95)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ("cluster", "kubeconfig", "private", "image", "repository"):
        parser.add_argument("--" + name, required=True)
    prepare(parser.parse_args())
