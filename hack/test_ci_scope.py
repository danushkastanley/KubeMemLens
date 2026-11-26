import subprocess
import sys
import tempfile
import unittest
from pathlib import Path
from ci_scope import OPT_IN_VERIFIERS, needs_kind


class ScopeTest(unittest.TestCase):
    def test_source_and_fixture_prefixes_trigger_kind(self):
        for path in ("internal/resourcemetrics/source.go", "cmd/app/main.go",
                     "charts/kube-memlens/templates/service.yaml",
                     "hack/fixtures/metrics-api/main.go", "hack/kind-profiles/memory-qos.yaml",
                     "hack/verify-resource-metrics-kind.sh", "go.mod", "Dockerfile",
                     ".github/workflows/node-context.yml"):
            with self.subTest(path=path):
                self.assertTrue(needs_kind([path]))

    def test_docs_and_similarly_named_paths_do_not_trigger(self):
        self.assertFalse(needs_kind(["README.md", "docs/api.md", "internal-not-code.md",
                                     "Dockerfile.backup", "hack/unrelated.sh"]))

    def test_chart_readme_alone_does_not_trigger(self):
        self.assertFalse(needs_kind(["charts/kube-memlens/README.md", "docs/installation.md"]))

    def test_chart_readme_with_chart_change_still_triggers(self):
        self.assertTrue(needs_kind(["charts/kube-memlens/README.md",
                                    "charts/kube-memlens/values.yaml"]))

    def test_opt_in_verifiers_alone_do_not_trigger(self):
        for path in OPT_IN_VERIFIERS:
            with self.subTest(path=path):
                self.assertFalse(needs_kind([path]))

    def test_opt_in_verifier_with_source_change_still_triggers(self):
        self.assertTrue(needs_kind(["hack/verify-evidence-discovery-kind.sh",
                                    "internal/tui/filter.go"]))

    def test_no_workflow_runs_an_opt_in_verifier(self):
        workflows = Path(__file__).resolve().parents[1] / ".github/workflows"
        for workflow in sorted(workflows.glob("*.yml")):
            text = workflow.read_text()
            for script, names in OPT_IN_VERIFIERS.items():
                for name in names:
                    with self.subTest(workflow=workflow.name, name=name):
                        self.assertFalse(name in text, f"{workflow.name} mentions {name}: remove {script} "
                                         "from OPT_IN_VERIFIERS in hack/ci_scope.py so that a change "
                                         "to it runs the kind lanes")

    def test_nul_input_preserves_unusual_source_names(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "paths"
            path.write_bytes(b"README.md\0internal/odd\nname.go\0")
            result = subprocess.run([sys.executable, str(Path(__file__).with_name("ci_scope.py")),
                                     str(path)], capture_output=True, text=True, check=True)
            self.assertEqual(result.stdout.strip(), "true")

    def test_missing_input_fails_instead_of_skipping_checks(self):
        result = subprocess.run([sys.executable, str(Path(__file__).with_name("ci_scope.py")),
                                 "/nonexistent/kube-memlens-ci-paths"], capture_output=True)
        self.assertNotEqual(result.returncode, 0)


if __name__ == "__main__":
    unittest.main()
