"""Regression check for public CSR transfer through a Docker tmpfs mount."""

from common import load, require
from prepare_execution_kind import transfer_public
from process import execute


def check():
    image = load("hack/node-qualification/profiles/kind-137.json")["nodeImage"]
    container = execute(["docker", "run", "--detach", "--network", "none", "--memory", "32m", "--cap-drop", "ALL",
                         "--read-only", "--tmpfs", "/tmp:rw,nosuid,nodev,noexec,size=1m", "--entrypoint", "sh", image,
                         "-c", "sleep 120"], timeout=30).strip()
    try:
        execute(["docker", "exec", container, "openssl", "req", "-new", "-newkey", "rsa:2048", "-nodes",
                 "-subj", "/CN=owned-fixture", "-keyout", "/tmp/fixture.key", "-out", "/tmp/fixture.csr"])
        transfer_public(container, "/tmp/fixture.csr", container, "/tmp/copied.csr")
        before = execute(["docker", "exec", container, "cat", "/tmp/fixture.csr"])
        after = execute(["docker", "exec", container, "cat", "/tmp/copied.csr"])
        require(before == after and before.startswith("-----BEGIN CERTIFICATE REQUEST-----"), "CSR transfer changed the public bytes")
    finally:
        execute(["docker", "rm", "--force", container], timeout=15)
    print("PASS public CSR bytes transferred through tmpfs without exposing a private key")


if __name__ == "__main__":
    check()
