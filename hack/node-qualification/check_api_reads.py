"""Reproduce concurrent container reads on the owned two-Node fixture without qualification."""

import argparse
import time
from concurrent.futures import ThreadPoolExecutor

from common import ContractError, privacy, require, write_new
from local_execution import prepare


def observe(runtime, expected):
    try:
        ready, mapped = runtime.workload()
        return "passed" if ready == mapped == expected else "incomplete-mapping"
    except ContractError as error:
        # Only the bridge's static categories can enter a shared diagnostic.
        categories = {"authentication", "forbidden", "not-found", "rate-limited", "server-unavailable",
                      "deadline", "cancelled", "transport", "invalid-response"}
        prefix = "qualification API read failed: "
        message = str(error)
        category = message.removeprefix(prefix)
        return category if message.startswith(prefix) and category in categories else "observation-failed"


def run(args):
    execution, profile, _ = prepare(args)
    observations = []
    try:
        execution.prepare()
        with ThreadPoolExecutor(max_workers=len(execution.runtimes)) as pool:
            for batch in range(40):
                execution.verify_binding()
                results = list(pool.map(lambda r: observe(r, profile["workload"]["containers"]), execution.runtimes))
                observations.append({"batch": batch, "results": results})
                print("concurrent API read batch " + str(batch + 1) + "/40: " + ", ".join(results), flush=True)
                time.sleep(2)
    finally:
        execution.cleanup()
    passed = all(state == "passed" for batch in observations for state in batch["results"])
    result = {"schemaVersion": 1, "scope": "local-concurrent-api-read-diagnostic", "qualified": False,
              "profile": {"id": profile["id"], "digest": profile["profileDigest"]}, "linuxNodes": len(execution.runtimes),
              "observations": observations, "readChecksPassed": passed, "cleanup": "passed"}
    privacy(result)
    write_new(args.output, result)
    require(passed, "concurrent API read diagnostic observed a failure")


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ("kubeconfig", "context", "private", "output", "profile", "image-repository", "image-digest"):
        parser.add_argument("--" + name, required=True)
    run(parser.parse_args())
