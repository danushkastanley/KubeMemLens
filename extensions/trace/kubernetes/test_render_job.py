import unittest
from render_job import job

IMAGE = "local/preflight@sha256:" + "a" * 64


class JobTests(unittest.TestCase):
    def test_fixed_security_and_lifetime(self):
        manifest = job("node-one", IMAGE, "preflight-one")
        spec = manifest["spec"]
        pod = spec["template"]["spec"]
        container = pod["containers"][0]
        self.assertEqual(spec["backoffLimit"], 0)
        self.assertEqual(spec["activeDeadlineSeconds"], 30)
        self.assertFalse(pod["automountServiceAccountToken"])
        self.assertFalse(pod["hostPID"])
        self.assertFalse(pod["hostNetwork"])
        self.assertEqual(container["securityContext"]["capabilities"],
                         {"drop": ["ALL"], "add": ["BPF", "PERFMON"]})
        self.assertTrue(all(m["readOnly"] for m in container["volumeMounts"]))
        self.assertNotIn("/proc", [m["mountPath"] for m in container["volumeMounts"]])
        self.assertEqual(container["imagePullPolicy"], "Never")

    def test_mutable_images_and_profile_substitution_are_rejected(self):
        for image in ["local/preflight:latest", "local/preflight@sha256:123", IMAGE + "\n"]:
            with self.assertRaises(ValueError):
                job("node", image, "job")
        with self.assertRaises(ValueError):
            job("node", IMAGE, "job", "unconfined")
        with self.assertRaises(ValueError):
            job("../node", IMAGE, "job")


if __name__ == "__main__":
    unittest.main()
