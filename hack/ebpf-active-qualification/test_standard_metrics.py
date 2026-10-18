import json
import unittest

from standard_metrics import AGENT, AGENT_FIELDS, COLLECTOR, agent_metrics, collector_metrics


def agent():
    values = {name: "1" for name in AGENT_FIELDS}
    values.update(last_scan_timestamp_seconds="1790670000", last_scan_duration_seconds="0.003000001")
    return "\n".join(AGENT + key + " " + value for key, value in values.items()) + "\n# EOF\n"


def collector():
    return (COLLECTOR + 'last_received_timestamp_seconds 0\n' +
            COLLECTOR + 'last_received_age_seconds 0\n' +
            COLLECTOR + 'requests_total{result="accepted"} 2\n' +
            COLLECTOR + 'last_duration_seconds 1.0000001e-3\n# EOF\n')


class StandardMetricsTests(unittest.TestCase):
    def test_real_metric_names_project_only_numeric_agent_fields(self):
        raw = agent().replace("# EOF", 'unrelated_metric{node="private-node"} 10\n# EOF')
        result = agent_metrics(raw)
        self.assertEqual(result["scanDurationNanos"], 3000001)
        self.assertEqual(result["scanCompletedUnixSeconds"], 1790670000)
        self.assertNotIn("private-node", json.dumps(result))
        self.assertTrue(all(type(value) is int for value in result.values()))

    def test_collector_absent_result_series_follow_explicit_renderer_contract(self):
        result = collector_metrics(collector())
        self.assertEqual(result["results"]["accepted"], 2)
        self.assertEqual(result["results"]["identity_rejected"], 0)
        self.assertEqual(result["durationNanos"], 1000001)  # Round upward, never understate.
        for raw in ("# EOF\n", COLLECTOR + "last_duration_seconds 0\n# EOF\n"):
            with self.assertRaises(ValueError):
                collector_metrics(raw)

    def test_missing_duplicate_unknown_or_nonfinite_metrics_fail(self):
        for bad in ("NaN", "+Inf", "-1", "1e999", "1 1790670000"):
            with self.subTest(bad=bad), self.assertRaises(ValueError):
                agent_metrics(agent().replace("0.003000001", bad))
        for raw in (agent().replace("# EOF\n", ""), "# EOF\n" + agent(),
                    agent().replace(AGENT + "metadata_cache_pods 1\n", ""),
                    agent().replace("# EOF", AGENT + "metadata_cache_pods 1\n# EOF"),
                    agent().replace('scans_total{result="success"}', 'scans_total{result="private"}'),
                    agent().replace("0.003000001", "0"),
                    agent().replace("metadata_cache_pods 1", "metadata_cache_pods 1.5"),
                    agent().replace("metadata_cache_pods 1", "metadata_cache_pods 9007199254740992")):
            with self.subTest(raw=raw[:32]), self.assertRaises(ValueError):
                agent_metrics(raw)
        with self.assertRaises(ValueError):
            collector_metrics(collector().replace('result="accepted"', 'result="private"'))
        with self.assertRaises(ValueError):
            collector_metrics(collector().replace("1.0000001e-3", "0"))

    def test_response_and_duration_bounds(self):
        for raw in ("# " + "x" * (1 << 20) + "\n" + agent(), "\n" * 8193 + agent(),
                    agent().replace("0.003000001", "1800.000000001")):
            with self.assertRaises(ValueError):
                agent_metrics(raw)
        self.assertEqual(agent_metrics(agent().replace("0.003000001", "1800"))["scanDurationNanos"], 1800000000000)


if __name__ == "__main__":
    unittest.main()
