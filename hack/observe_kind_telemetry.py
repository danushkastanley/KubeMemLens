#!/usr/bin/env python3
"""Collect sanitised KubeMemLens resource counters from kind nodes."""

from __future__ import annotations

import argparse
import json
import re
import subprocess
import sys
from dataclasses import dataclass
from pathlib import Path, PurePosixPath
from typing import Any, Callable, Sequence


COMPONENTS = ("agent", "collector")
CONTAINER_ID = re.compile(r"^[a-f0-9]{64}$")
DOCKER_NAME = re.compile(r"^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$")
NAMESPACE = re.compile(r"^[a-z0-9]([a-z0-9.-]{0,251}[a-z0-9])?$")
CGROUP_ROOT = "/sys/fs/cgroup"
COMMAND_TIMEOUT_SECONDS = 10


class ObservationError(RuntimeError):
    """Raised when runtime telemetry is incomplete or ambiguous."""


Runner = Callable[[Sequence[str], int], str]


def run_command(argv: Sequence[str], timeout: int) -> str:
    try:
        result = subprocess.run(
            list(argv),
            check=True,
            capture_output=True,
            text=True,
            timeout=timeout,
        )
    except (OSError, subprocess.SubprocessError) as error:
        raise ObservationError("runtime telemetry command failed") from error
    return result.stdout


@dataclass(frozen=True)
class ContainerSample:
    container_id: str
    component: str
    working_set_bytes: int
    memory_limit_bytes: int
    cpu_usage_usec: int
    cpu_throttled_usec: int
    cpu_quota_configured: bool


class KindTelemetryObserver:
    def __init__(self, runner: Runner = run_command, previous: dict[str, tuple[int, int]] | None = None) -> None:
        self.runner = runner
        self.previous = previous or {}
        self.current: dict[str, tuple[int, int]] = {}

    def observe(self, nodes: Sequence[str], namespace: str) -> dict[str, Any]:
        self._validate_inputs(nodes, namespace)
        samples: list[ContainerSample] = []
        for node in nodes:
            samples.extend(self._observe_node(node, namespace))
        observed = {sample.component for sample in samples}
        if observed != set(COMPONENTS):
            raise ObservationError("agent or collector telemetry is missing")
        self.current = {
            sample.container_id: (sample.cpu_usage_usec, sample.cpu_throttled_usec)
            for sample in samples
        }
        return {
            "schemaVersion": 1,
            "components": {
                component: self._aggregate(samples, component)
                for component in COMPONENTS
            },
        }

    def _observe_node(self, node: str, namespace: str) -> list[ContainerSample]:
        containers = self._json_command(node, "crictl", "ps", "-o", "json")
        stats = self._json_command(node, "crictl", "stats", "-o", "json")
        stats_by_id = self._stats_by_id(stats)
        result: list[ContainerSample] = []
        for container in self._target_containers(containers, namespace):
            container_id, component = container
            stat = stats_by_id.get(container_id)
            if stat is None:
                raise ObservationError("runtime stats are missing for a selected component")
            inspect = self._json_command(
                node, "crictl", "inspect", "-o", "json", container_id
            )
            memory_limit, quota = self._inspect_limits(inspect)
            working_set = self._nested_integer(
                stat, ("memory", "workingSetBytes", "value"), "working set"
            )
            cpu_usage, cpu_throttled = self._cpu_stat(node, container_id)
            result.append(
                ContainerSample(
                    container_id=container_id,
                    component=component,
                    working_set_bytes=working_set,
                    memory_limit_bytes=memory_limit,
                    cpu_usage_usec=cpu_usage,
                    cpu_throttled_usec=cpu_throttled,
                    cpu_quota_configured=quota,
                )
            )
        return result

    def _json_command(self, node: str, *command: str) -> dict[str, Any]:
        raw = self.runner(
            ("docker", "exec", node, *command), COMMAND_TIMEOUT_SECONDS
        )
        try:
            value = json.loads(raw)
        except (json.JSONDecodeError, TypeError) as error:
            raise ObservationError("runtime command returned malformed JSON") from error
        if not isinstance(value, dict):
            raise ObservationError("runtime command returned an invalid JSON shape")
        return value

    @staticmethod
    def _target_containers(
        document: dict[str, Any], namespace: str
    ) -> list[tuple[str, str]]:
        containers = document.get("containers")
        if not isinstance(containers, list):
            raise ObservationError("runtime container list is missing")
        selected: list[tuple[str, str]] = []
        for item in containers:
            if not isinstance(item, dict) or not isinstance(item.get("labels"), dict):
                raise ObservationError("runtime container metadata is malformed")
            labels = item["labels"]
            if labels.get("io.kubernetes.pod.namespace") != namespace:
                continue
            component = labels.get("io.kubernetes.container.name")
            if component not in COMPONENTS:
                continue
            container_id = item.get("id")
            if not isinstance(container_id, str) or not CONTAINER_ID.fullmatch(container_id):
                raise ObservationError("selected runtime container has an invalid full ID")
            selected.append((container_id, component))
        return selected

    @staticmethod
    def _stats_by_id(document: dict[str, Any]) -> dict[str, dict[str, Any]]:
        stats = document.get("stats")
        if not isinstance(stats, list):
            raise ObservationError("runtime stats list is missing")
        result: dict[str, dict[str, Any]] = {}
        for stat in stats:
            if not isinstance(stat, dict):
                raise ObservationError("runtime stats entry is malformed")
            attributes = stat.get("attributes")
            container_id = attributes.get("id") if isinstance(attributes, dict) else None
            if isinstance(container_id, str):
                result[container_id] = stat
        return result

    def _inspect_limits(self, document: dict[str, Any]) -> tuple[int, bool]:
        resources = self._nested(
            document, ("info", "runtimeSpec", "linux", "resources"), "resources"
        )
        memory = resources.get("memory")
        if not isinstance(memory, dict):
            raise ObservationError("runtime memory limit is missing")
        memory_limit = self._integer(memory.get("limit"), "memory limit")
        if memory_limit <= 0:
            raise ObservationError("runtime memory limit must be greater than zero")
        cpu = resources.get("cpu", {})
        if not isinstance(cpu, dict):
            raise ObservationError("runtime CPU resources are malformed")
        quota_value = cpu.get("quota")
        quota = False
        if quota_value is not None:
            quota_limit = self._signed_integer(quota_value, "CPU quota")
            if quota_limit < -1:
                raise ObservationError("runtime CPU quota is malformed")
            quota = quota_limit > 0
        return memory_limit, quota

    def _cpu_stat(self, node: str, container_id: str) -> tuple[int, int]:
        paths = self.runner(
            (
                "docker",
                "exec",
                node,
                "find",
                CGROUP_ROOT,
                "-xdev",
                "-maxdepth",
                "12",
                "-type",
                "f",
                "-name",
                "cpu.stat",
                "-path",
                f"*{container_id}*",
                "-print",
            ),
            COMMAND_TIMEOUT_SECONDS,
        ).splitlines()
        paths = [path.strip() for path in paths if path.strip()]
        if len(paths) != 1 or not self._valid_cpu_stat_path(paths[0], container_id):
            raise ObservationError("container cpu.stat path is missing or ambiguous")
        raw = self.runner(
            ("docker", "exec", node, "cat", paths[0]), COMMAND_TIMEOUT_SECONDS
        )
        values: dict[str, int] = {}
        for line in raw.splitlines():
            fields = line.split()
            if len(fields) != 2 or fields[0] in values:
                raise ObservationError("container cpu.stat is malformed")
            values[fields[0]] = self._integer(fields[1], "cpu.stat value")
        if "usage_usec" not in values or "throttled_usec" not in values:
            raise ObservationError("container cpu.stat is incomplete")
        return values["usage_usec"], values["throttled_usec"]

    @staticmethod
    def _valid_cpu_stat_path(path: str, container_id: str) -> bool:
        candidate = PurePosixPath(path)
        return (
            path.startswith(CGROUP_ROOT + "/")
            and candidate.name == "cpu.stat"
            and container_id in path
            and ".." not in candidate.parts
        )

    def _aggregate(self, samples: Sequence[ContainerSample], component: str) -> dict[str, Any]:
        selected = [sample for sample in samples if sample.component == component]
        usage = sum(sample.cpu_usage_usec for sample in selected)
        throttled = sum(sample.cpu_throttled_usec for sample in selected)
        quota_configured = any(sample.cpu_quota_configured for sample in selected)
        memory_ratio = max(sample.working_set_bytes / sample.memory_limit_bytes for sample in selected)
        throttle_ratios = []
        for sample in selected:
            prior = self.previous.get(sample.container_id)
            if prior is None:
                continue
            usage_delta = sample.cpu_usage_usec - prior[0]
            throttled_delta = sample.cpu_throttled_usec - prior[1]
            if usage_delta < 0 or throttled_delta < 0 or (usage_delta == 0 and throttled_delta > 0):
                raise ObservationError("runtime CPU counters reset or are inconsistent")
            throttle_ratios.append(0.0 if usage_delta == 0 else throttled_delta / usage_delta)
        return {
            "containerCount": len(selected),
            "workingSetBytes": sum(sample.working_set_bytes for sample in selected),
            "memoryLimitBytes": sum(sample.memory_limit_bytes for sample in selected),
            "maximumMemoryLimitRatio": round(memory_ratio, 8),
            "cpuUsageUsec": usage,
            "cpuThrottledUsec": throttled,
            "cpuQuotaConfigured": quota_configured,
            "maximumCPUThrottlingRatio": round(max(throttle_ratios), 8) if throttle_ratios else None,
        }

    @staticmethod
    def _nested(document: dict[str, Any], path: Sequence[str], label: str) -> dict[str, Any]:
        value: Any = document
        for key in path:
            value = value.get(key) if isinstance(value, dict) else None
        if not isinstance(value, dict):
            raise ObservationError(f"runtime {label} are missing")
        return value

    def _nested_integer(
        self, document: dict[str, Any], path: Sequence[str], label: str
    ) -> int:
        value: Any = document
        for key in path:
            value = value.get(key) if isinstance(value, dict) else None
        return self._integer(value, label)

    @staticmethod
    def _integer(value: Any, label: str) -> int:
        result = KindTelemetryObserver._signed_integer(value, label)
        if result < 0:
            raise ObservationError(f"runtime {label} is malformed")
        return result

    @staticmethod
    def _signed_integer(value: Any, label: str) -> int:
        if isinstance(value, bool):
            raise ObservationError(f"runtime {label} is malformed")
        try:
            result = int(value)
        except (TypeError, ValueError) as error:
            raise ObservationError(f"runtime {label} is malformed") from error
        if str(value).strip() != str(result):
            raise ObservationError(f"runtime {label} is malformed")
        return result

    @staticmethod
    def _validate_inputs(nodes: Sequence[str], namespace: str) -> None:
        if not nodes or len(nodes) > 100:
            raise ObservationError("one to 100 kind node containers are required")
        if len(set(nodes)) != len(nodes):
            raise ObservationError("kind node container names must be unique")
        if any(not DOCKER_NAME.fullmatch(node) for node in nodes):
            raise ObservationError("kind node container name is invalid")
        if not NAMESPACE.fullmatch(namespace):
            raise ObservationError("Kubernetes namespace is invalid")


def load_state(path: str | None) -> dict[str, tuple[int, int]]:
    if path is None or not Path(path).exists():
        return {}
    try:
        document = json.loads(Path(path).read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as error:
        raise ObservationError("runtime telemetry state is unreadable") from error
    if not isinstance(document, dict):
        raise ObservationError("runtime telemetry state is malformed")
    state: dict[str, tuple[int, int]] = {}
    for container_id, counters in document.items():
        if not isinstance(container_id, str) or not CONTAINER_ID.fullmatch(container_id):
            raise ObservationError("runtime telemetry state contains an invalid container ID")
        if not isinstance(counters, list) or len(counters) != 2:
            raise ObservationError("runtime telemetry state contains malformed counters")
        usage = KindTelemetryObserver._integer(counters[0], "state CPU usage")
        throttled = KindTelemetryObserver._integer(counters[1], "state CPU throttling")
        state[container_id] = (usage, throttled)
    return state


def write_state(path: str | None, state: dict[str, tuple[int, int]]) -> None:
    if path is None:
        return
    target = Path(path)
    target.write_text(json.dumps(state, sort_keys=True, separators=(",", ":")) + "\n", encoding="utf-8")
    target.chmod(0o600)


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--node", action="append", required=True, help="kind node container name")
    parser.add_argument("--namespace", required=True, help="KubeMemLens namespace")
    parser.add_argument("--state-file", help="local private counter state for interval deltas")
    args = parser.parse_args()
    try:
        observer = KindTelemetryObserver(previous=load_state(args.state_file))
        result = observer.observe(args.node, args.namespace)
        write_state(args.state_file, observer.current)
    except ObservationError as error:
        print(f"kind telemetry observation failed: {error}", file=sys.stderr)
        return 1
    json.dump(result, sys.stdout, sort_keys=True, separators=(",", ":"))
    sys.stdout.write("\n")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
