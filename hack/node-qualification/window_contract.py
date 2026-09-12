"""Bind complete baseline and enabled pool windows to one measurement protocol."""

from common import exact, instant, privacy, require
from evidence import validate_rotation, validate_samples


def validate_window(profile, window, phase):
    exact(window, {"schemaVersion", "phase", "startedAt", "completedAt", "profile", "observation", "nodes"}, "measurement window")
    privacy(window)
    require(type(window["schemaVersion"]) is int and window["schemaVersion"] == 2 and window["phase"] == phase,
            "measurement window version or phase mismatch")
    require(window["profile"] == {"id": profile["id"], "digest": profile["profileDigest"]}, "measurement profile mismatch")
    require(window["observation"] == {"method": "kubernetes-probes-v1", "image": profile["workload"]["image"]},
            "measurement observation protocol mismatch")
    require(instant(window["completedAt"]) >= instant(window["startedAt"]), "measurement timestamps are reversed")
    require(isinstance(window["nodes"], list) and len(window["nodes"]) == profile["workload"]["linuxNodes"],
            "measurement pool differs from the profile")
    for slot, node in enumerate(window["nodes"]):
        exact(node, {"slot", "samples", "rotation"}, "measurement slot")
        require(type(node["slot"]) is int and node["slot"] == slot, "measurement slots changed order")
        validate_samples(node["samples"], phase)
        validate_rotation(node["rotation"])
    return window


def join_windows(profile, baseline, enabled):
    validate_window(profile, baseline, "baseline")
    validate_window(profile, enabled, "enabled")
    require(instant(baseline["completedAt"]) <= instant(enabled["startedAt"]), "measurement phases overlap or are reversed")
    return {"schemaVersion": 2, "observation": dict(enabled["observation"]),
            "nodes": [{"slot": slot, "samples": {"baseline": before["samples"], "enabled": after["samples"]},
                       "rotation": after["rotation"]}
                      for slot, (before, after) in enumerate(zip(baseline["nodes"], enabled["nodes"]))]}
