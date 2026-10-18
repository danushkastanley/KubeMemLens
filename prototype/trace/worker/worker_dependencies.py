"""Enforce the fixed worker's dependency boundary without running the worker."""
import os
from pathlib import Path
import subprocess

FORBIDDEN = (
    "google.golang.org/grpc",
    "k8s.io/client-go/",
    "k8s.io/cli-runtime/",
    "sigs.k8s.io/kustomize/",
    "github.com/inspektor-gadget/inspektor-gadget/pkg/container-collection",
    "github.com/inspektor-gadget/inspektor-gadget/pkg/networktracer",
    "github.com/inspektor-gadget/inspektor-gadget/pkg/tchandler",
    "github.com/inspektor-gadget/inspektor-gadget/pkg/uprobetracer",
    "github.com/inspektor-gadget/inspektor-gadget/pkg/socketenricher",
)


def verify(dependencies):
    names = dependencies.splitlines()
    if not names:
        raise ValueError("worker dependency inventory is empty")
    for name in names:
        if any(name == prefix.rstrip("/") or name.startswith(prefix.rstrip("/") + "/")
               for prefix in FORBIDDEN):
            raise ValueError("worker links an unsupported package: " + name)


if __name__ == "__main__":
    root = Path(__file__).resolve().parent
    for architecture in ("arm64", "amd64"):
        env = dict(os.environ, CGO_ENABLED="0", GOOS="linux", GOARCH=architecture)
        result = subprocess.run(["go", "list", "-mod=readonly", "-deps",
                                 "./cmd/memlens-filecache-worker"], cwd=root, env=env,
                                check=True, capture_output=True, text=True, timeout=120)
        verify(result.stdout)
    print("Worker dependency boundary passed for Linux arm64 and amd64.")
