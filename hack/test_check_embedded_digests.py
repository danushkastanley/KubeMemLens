import unittest

from check_embedded_digests import classify, collect, references, scanned

OLD = "sha256:" + "a" * 64
NEW = "sha256:" + "b" * 64


class EmbeddedDigestTest(unittest.TestCase):
    def test_finds_pins_in_run_lines_and_templates(self):
        text = f"""
          run: docker run --rm ghcr.io/aquasecurity/trivy:0.72.0@{OLD} image x
          node-image: kindest/node:v1.35.5@{OLD}
          image: public.ecr.aws/docker/library/busybox@{NEW}
          uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
        """
        self.assertEqual(references(text), [
            ("ghcr.io/aquasecurity/trivy", "0.72.0", OLD),
            ("kindest/node", "v1.35.5", OLD),
            ("public.ecr.aws/docker/library/busybox", None, NEW),
        ])

    def test_scans_what_dependabot_does_not(self):
        for path in (".github/workflows/ci.yml", "charts/kube-memlens/templates/tests/test-connection.yaml",
                     "hack/e2e-kind.sh", "Makefile"):
            self.assertTrue(scanned(path), path)
        for path in ("Dockerfile", "extensions/trace/Dockerfile", "hack/terminal-qualification/Dockerfile.linux",
                     "docs/installation.md", "hack/test_check_embedded_digests.py", "internal/x/testdata/a.yaml"):
            self.assertFalse(scanned(path), path)

    def test_a_moved_tag_is_stale_and_names_every_file(self):
        files = {".github/workflows/ci.yml": f"trivy:0.72.0@{OLD}\nhelm:3.18.4@{OLD}",
                 "hack/e2e-kind.sh": f"trivy:0.72.0@{OLD}", "docs/x.md": f"ignored:1@{OLD}"}
        pins = collect(files, files.get)
        report = classify(pins, lambda name, tag: NEW if name == "trivy" else OLD)
        self.assertEqual([(e["image"], e["paths"]) for e in report["stale"]],
                         [("trivy", [".github/workflows/ci.yml", "hack/e2e-kind.sh"])])
        self.assertEqual([e["image"] for e in report["current"]], ["helm"])

    def test_unreadable_and_untagged_are_reported_not_passed(self):
        pins = {("gone", "1", OLD): ["a.yml"], ("busybox", None, OLD): ["b.yaml"]}
        report = classify(pins, lambda name, tag: None)
        self.assertEqual([e["image"] for e in report["unreadable"]], ["gone"])
        self.assertEqual([e["image"] for e in report["untagged"]], ["busybox"])
        self.assertEqual(report["current"], [])


if __name__ == "__main__":
    unittest.main()
