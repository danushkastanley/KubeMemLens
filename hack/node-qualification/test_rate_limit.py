import json
import subprocess
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

from common import ContractError
from kind_runtime import KindRuntime, command
from kubernetes_runtime import API_FAILURE_CODES, KubernetesRuntime
from rate_limit import ATTEMPTS, RETRY_AFTER_SECONDS, RateLimited


class Refusals:
    """A read that is refused a fixed number of times before it succeeds."""

    def __init__(self, refusals, error):
        self.remaining, self.error, self.calls = refusals, error, 0

    def __call__(self, argv, **_):
        self.calls += 1
        if self.remaining > 0:
            self.remaining -= 1
            raise self.error
        return json.dumps({"store": {}, "items": []})


def kind_runtime(folder, run):
    runtime = object.__new__(KindRuntime)
    runtime.private = Path(folder)
    (runtime.private / "api-server").write_text("https://127.0.0.1:6443")
    runtime.run = run
    return runtime


def kubernetes_runtime(run):
    return KubernetesRuntime("/private", "fixture", "fixture", "uid", "node", "/bridge", run=run)


class RateLimitTest(unittest.TestCase):
    def test_curl_refusal_with_429_is_a_rate_limit(self):
        error = subprocess.CalledProcessError(22, ["curl"], stderr=b"curl: (22) The requested URL returned error: 429")
        with patch("kind_runtime.subprocess.run", side_effect=error), self.assertRaises(RateLimited) as caught:
            command(["curl"])
        self.assertEqual(str(caught.exception), "local command failed: http-429")

    def test_both_runtimes_wait_and_read_again_after_a_rate_limit(self):
        with tempfile.TemporaryDirectory() as folder:
            cases = (
                ("kind", lambda run: kind_runtime(folder, run), RateLimited("local command failed: http-429")),
                ("kubernetes", kubernetes_runtime, ContractError(API_FAILURE_CODES[13])),
            )
            for name, build, error in cases:
                with self.subTest(runtime=name), patch("rate_limit.time.sleep") as sleep:
                    run = Refusals(ATTEMPTS - 1, error)
                    self.assertEqual(build(run).api("/clusterstatus/current")["store"], {})
                    self.assertEqual(run.calls, ATTEMPTS)
                    self.assertEqual([call.args for call in sleep.call_args_list], [(RETRY_AFTER_SECONDS,)] * (ATTEMPTS - 1))

    def test_a_read_refused_on_every_attempt_fails_with_the_rate_limit(self):
        with tempfile.TemporaryDirectory() as folder:
            cases = (
                ("kind", lambda run: kind_runtime(folder, run), RateLimited("local command failed: http-429"),
                 "fixture API read clusterstatus: local command failed: http-429"),
                ("kubernetes", kubernetes_runtime, ContractError(API_FAILURE_CODES[13]), API_FAILURE_CODES[13]),
            )
            for name, build, error, message in cases:
                with self.subTest(runtime=name), patch("rate_limit.time.sleep"):
                    run = Refusals(ATTEMPTS, error)
                    with self.assertRaises(ContractError) as caught:
                        build(run).api("/clusterstatus/current")
                    self.assertEqual(str(caught.exception), message)
                    self.assertEqual(run.calls, ATTEMPTS)

    def test_other_failures_are_not_read_again(self):
        with tempfile.TemporaryDirectory() as folder:
            cases = (
                ("kind", lambda run: kind_runtime(folder, run), ContractError("local command failed: http-503")),
                ("kubernetes", kubernetes_runtime, ContractError(API_FAILURE_CODES[14])),
            )
            for name, build, error in cases:
                with self.subTest(runtime=name), patch("rate_limit.time.sleep") as sleep:
                    run = Refusals(1, error)
                    with self.assertRaises(ContractError):
                        build(run).api("/clusterstatus/current")
                    self.assertEqual((run.calls, sleep.call_count), (1, 0))


if __name__ == "__main__":
    unittest.main()
