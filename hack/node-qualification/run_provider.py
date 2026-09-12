"""Run an explicitly approved provider proposal; cleanup and review remain separate gates."""

import argparse
import copy
import signal
import sys

from common import ContractError, load, privacy, require, utc_text, write_new
from evaluate import evaluate
from kubernetes_commands import KubernetesCommands
from kubernetes_runtime import API_FAILURE_CODES
from lifecycle import event
from live_images import verify as verify_images
from measurement_checks import measurement_checks
from network_probes import NetworkChecks
from provider_bundle import validate_bundle
from provider_cli import verify as verify_cli
from provider_execution import Execution
from provider_inputs import prepare, build_helpers
from provider_inventory import collect_inventory
from provider_record import assemble, cleanup_cluster
from provider_probes import PROBE_FAILURES
from provider_recovery import PoolRecovery
from provider_replacement import ProviderReplacement


def publish(path, document):
    privacy(document)
    write_new(path, document)


def measured_windows_pass(execution):
    for node in execution.measurements()["nodes"]:
        checks = measurement_checks(execution.bundle.profile, node["samples"], node["rotation"], 1)
        require(all(check["passed"] for group in checks.values() for check in group), "fixed measurement checks failed")


def failure_reason(error):
    known = {message: "api-" + message.rsplit(": ", 1)[1] for message in API_FAILURE_CODES.values()}
    known.update({message: "kubelet-" + message.rsplit(": ", 1)[1] for message in PROBE_FAILURES.values()})
    return known.get(str(error), "unclassified")


def run(args):
    bundle, proof, private, public = prepare(args)
    execution, record, failure = None, None, None
    stage, cleanup = "helpers", "not-started"
    started = utc_text()
    lifecycle = {name: event() for name in ("sourceLoss", "agentRestart", "collectorRestart", "nodeIdentityReplacement", "providerNodeReplacement")}
    try:
        build_helpers(private)
        # Outputs must not make the immutable tool checkout dirty. Recheck before
        # the first provider/Kubernetes command as well as after collection.
        validate_bundle(args.proposal, bundle.profile, args.plan_digest)
        stage = "inventory"
        receipt, binding = collect_inventory(bundle)
        require(binding["runtime"]["architecture"] == proof["image"]["architecture"], "live pool architecture differs from the verified image")
        publish(public / "provider-inventory.json", receipt)
        stage = "preparation"
        execution = Execution(bundle, binding, KubernetesCommands(bundle.configuration["kubeconfigPath"], bundle.configuration["context"]),
                              private, str(private / "chart-inventory"), private / "api-bridge", proof["image"])
        execution.prepare()
        for phase in ("baseline", "enabled"):
            stage = phase
            if phase == "enabled":
                execution.enable()
            publish(public / (phase + "-measurements.json"), execution.measure(phase))
        stage = "measurement-validation"
        measured_windows_pass(execution)
        recovery = PoolRecovery(execution)
        for name, observe in (("sourceLoss", recovery.source_loss), ("agentRestart", lambda: recovery.restart("agent")),
                              ("collectorRestart", lambda: recovery.restart("collector"))):
            stage = name
            lifecycle[name] = observe()
            publish(public / (name + ".json"), lifecycle[name])
            require(lifecycle[name]["state"] == "passed", "pool recovery check failed")
        stage = "replacement"
        replacement = ProviderReplacement(execution, receipt).run(args.replacement_slot, args.replacement_acknowledge)
        # One observed machine replacement also proves its Node UID replacement.
        lifecycle["providerNodeReplacement"] = replacement
        lifecycle["nodeIdentityReplacement"] = copy.deepcopy(replacement)
        publish(public / "replacement.json", {"event": replacement, "observations": execution.replacement})
        stage = "network-policy"
        network = NetworkChecks(execution).run()
        publish(public / "network-policy.json", network)
        require(network["passed"] is True, "provider NetworkPolicy verification failed")
        stage = "production-cli"
        publish(public / "production-cli.json", verify_cli(execution))
        stage = "runtime-identity"
        publish(public / "final-live-images.json", verify_images(execution, proof["image"], "enabled"))
        stage = "record"
        validate_bundle(args.proposal, bundle.profile, args.plan_digest)
        record = assemble(execution, proof, receipt, started, lifecycle, network, "completed")
        publish(public / "qualification-observations.json", record)
    except (Exception, KeyboardInterrupt) as error:
        failure = error
    finally:
        if execution is not None:
            try:
                if record is not None:
                    record = cleanup_cluster(execution, record)
                    publish(public / "kubernetes-cleaned-observations.json", record)
                else:
                    execution.cleanup()
                cleanup = "passed"
            except (Exception, KeyboardInterrupt) as error:
                cleanup = "failed"
                if failure is None:
                    failure, stage = error, "cleanup"
    if failure is not None:
        publish(public / "failure.json", {"schemaVersion": 1, "scope": "provider-run-failure", "qualified": False,
                "planDigest": args.plan_digest, "stage": stage, "failureType": type(failure).__name__, "reason": failure_reason(failure), "ownedCleanup": cleanup,
                "providerCleanup": "pending", "startedAt": started, "completedAt": utc_text()})
        if isinstance(failure, KeyboardInterrupt):
            raise failure
        raise ContractError("provider run failed during " + stage + "; see bounded failure evidence and private ownership receipts") from failure
    result = evaluate(bundle.profile, record)
    publish(public / "qualification-evaluation.pending.json", result)
    checks = [c for c in result["checks"] if c["id"] != "cleanup"]
    checks += [c for node in result.get("nodeChecks", []) for c in node["checks"]]
    require(all(c["passed"] for c in checks), "provider measurements failed; cleanup confirmation cannot qualify them")
    return {"state": "awaiting-independent-provider-cleanup", "qualified": False, "recordDigest": record["recordDigest"]}


def interrupted(signum, frame):
    raise KeyboardInterrupt


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ("proposal", "profile", "plan-digest", "candidate-bundle", "candidate-tag", "image-archive", "output-dir",
                 "acknowledge", "replacement-acknowledge"):
        parser.add_argument("--" + name, required=True)
    parser.add_argument("--architecture", required=True, choices=("amd64", "arm64"))
    parser.add_argument("--replacement-slot", required=True, type=int)
    args = parser.parse_args()
    previous = signal.signal(signal.SIGTERM, interrupted)
    try:
        run(args)
        print("Provider measurements and Kubernetes cleanup completed; independent provider cleanup and review remain pending.")
        return 0
    except KeyboardInterrupt:
        print("Provider qualification interrupted; inspect cleanup evidence before another run.", file=sys.stderr)
        return 130
    except ContractError as error:
        print("Provider qualification failed: " + str(error), file=sys.stderr)
        return 2
    except (OSError, ValueError, KeyError, TypeError) as error:
        print("Provider qualification failed (" + type(error).__name__ + "); inspect any retained evidence and private receipts.", file=sys.stderr)
        return 2
    finally:
        signal.signal(signal.SIGTERM, previous)


if __name__ == "__main__":
    raise SystemExit(main())
