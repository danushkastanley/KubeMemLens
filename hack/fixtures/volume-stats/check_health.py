"""Check private fixture responses without printing runtime names or payloads."""

import json
import pathlib
import sys


def matches(document, case):
    if document.get("kind") != "PodVolumeContext":
        return False
    row = document["context"]["volumes"][0]
    if row["usage"]["availability"] != "reported":
        return False
    sources = {h["observation"]["source"]: h for h in row["health"]}
    if set(sources) != {"pod-node-plugin", "pvc-controller-plugin", "csi-node-backend"}:
        return False
    pod, controller, backend = (sources[k] for k in
                                ("pod-node-plugin", "pvc-controller-plugin", "csi-node-backend"))
    if any(h["observation"]["probeFreshness"] != "unknown" for h in sources.values()):
        return False
    if "r5-private-marker" in json.dumps(document):
        return False
    if case in {"off", "disabled"}:
        expected = "unreported" if case == "off" else "disabled"
        return all(h["observation"]["availability"] == expected and "lastGood" not in h
                   for h in sources.values())
    if case in {"pv-denied", "backend-denied"}:
        return (backend["observation"]["availability"] == "forbidden" and
                "lastGood" not in backend and
                (("driver" not in row) if case == "pv-denied" else
                 row.get("driver") == "hostpath.csi.k8s.io"))
    if case == "adverse":
        return (pod["observation"]["adverse"] and backend["observation"]["adverse"] and
                controller["observation"]["state"] == "healthy")
    if case in {"recovered", "schema4"}:
        current = (pod["observation"]["state"] == "healthy" and
                   controller["observation"]["adverse"] and
                   backend["observation"]["availability"] == "unreported")
        if case == "schema4":
            return current and "lastGood" not in backend
        old = backend.get("lastGood", {}).get("observation", {})
        return current and old.get("adverse") and old.get("state") == "stale"
    return False


def main():
    try:
        return 0 if matches(json.loads(pathlib.Path(sys.argv[1]).read_text()), sys.argv[2]) else 1
    except (ValueError, KeyError, IndexError, TypeError, OSError):
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
