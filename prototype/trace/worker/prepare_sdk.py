"""Materialise the exact worker SDK source without changing the Go module cache.

The module checksum identifies the upstream input; the checked-in patch identifies
the local changes. Existing generated source is reused only after full hash checks.
This operation compiles or loads no BPF and grants no programme acceptance.
"""
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile

ROOT = Path(__file__).resolve().parent
MODULE = "github.com/inspektor-gadget/inspektor-gadget@v0.56.0"
SUM = "h1:Ps4HXfpq4qUMrWlCSqdxl3ldP1LMF+tn9VHip637pAg="
RECEIPT = ".kml-source-receipt.json"


def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def files(path):
    return {str(p.relative_to(path)): sha(p) for p in sorted(path.rglob("*"))
            if p.is_file() and p.name != RECEIPT}


def prepare():
    os.umask(0o077)
    patch = ROOT / "sdk-policy.patch"
    target = ROOT / ".sdk"
    patch_sha = sha(patch)
    if target.exists():
        receipt = json.loads((target / RECEIPT).read_text())
        if receipt["moduleSum"] != SUM or receipt["patchSHA256"] != patch_sha or receipt["files"] != files(target):
            raise ValueError("existing SDK differs; preserve it before preparing a new copy")
        print("Verified the existing constrained SDK source.")
        return
    stage = Path(tempfile.mkdtemp(prefix=".sdk-stage-", dir=ROOT))
    environment = dict(os.environ, GOWORK="off", GO111MODULE="on")
    result = json.loads(subprocess.check_output(["go", "mod", "download", "-json", MODULE],
                                               cwd=stage, env=environment, timeout=180))
    if result.get("Sum") != SUM or result.get("Version") != "v0.56.0":
        raise ValueError("SDK module does not match the accepted checksum")
    source = stage / "source"
    shutil.copytree(result["Dir"], source, copy_function=shutil.copyfile)
    for directory in source.rglob("*"):
        if directory.is_dir():
            directory.chmod(0o700)
    source.chmod(0o700)
    subprocess.run(["git", "apply", "--check", str(patch)], cwd=source, check=True, timeout=15)
    subprocess.run(["git", "apply", str(patch)], cwd=source, check=True, timeout=15)
    if sha(patch) != patch_sha:
        raise ValueError("SDK patch changed during preparation")
    receipt = {"module": MODULE, "moduleSum": SUM, "patchSHA256": patch_sha, "files": files(source)}
    (source / RECEIPT).write_text(json.dumps(receipt, indent=2) + "\n")
    source.rename(target)
    stage.rmdir()
    print("Prepared the checksum-verified SDK with the explicit worker policy patch.")


if __name__ == "__main__":
    prepare()
