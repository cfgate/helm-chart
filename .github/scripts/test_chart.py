"""Exercise real Helm rendering and schema rejection without a cluster."""

import json
from pathlib import Path
import re
import shutil
import subprocess
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[2]
DIGEST = "sha256:3d3eaeae0ae0a76f8b3bc5271f642cf06f42e6525c85cb16e1b778fb4d864d6a"


class ChartTests(unittest.TestCase):
    def render(self, values=None, chart=".", deployment=True):
        with tempfile.NamedTemporaryFile(mode="w", suffix=".json") as f:
            json.dump(values or {}, f)
            f.flush()
            args = ["helm", "template", "cfgate", chart, "--namespace", "manager", "-f", f.name]
            if deployment:
                args += ["--show-only", "templates/deployment.yaml"]
            return subprocess.run(args, cwd=ROOT, capture_output=True, text=True)

    def image(self, values=None, chart="."):
        result = self.render(values, chart)
        self.assertEqual(result.returncode, 0, result.stderr)
        images = re.findall(r'^\s+image: "([^"]+)"$', result.stdout, re.M)
        self.assertEqual(len(images), 1)
        return images[0]

    def test_image_selection(self):
        custom = "sha256:" + "a" * 64
        for values, expected in [
            ({}, "ghcr.io/cfgate/cfgate@" + DIGEST),
            ({"image": {"tag": "custom"}}, "ghcr.io/cfgate/cfgate:custom"),
            ({"image": {"repository": "example.com/operator"}}, "example.com/operator:0.2.0-alpha.6"),
            ({"image": {"repository": "example.com/operator", "tag": "custom"}}, "example.com/operator:custom"),
            ({"image": {"digest": custom, "tag": "ignored"}}, "ghcr.io/cfgate/cfgate@" + custom),
            ({"image": {"repository": "example.com/operator", "digest": custom}}, "example.com/operator@" + custom),
        ]:
            with self.subTest(values=values):
                self.assertEqual(self.image(values), expected)

    def test_controller_namespace_and_service_account_overrides(self):
        result = self.render({
            "installCRDs": False,
            "namespaceOverride": "workloads",
            "controller": {"clusterDomain": "cluster.internal", "installationNamespace": "claims",
                           "cloudflareRequestTimeoutSeconds": 20, "maxIngressRules": 2000,
                           "maxConfigurationBytes": 2097152},
            "serviceAccount": {"create": False, "name": "external-manager"},
            "metrics": {"port": 9090}, "health": {"port": 9091},
        }, deployment=False)
        self.assertEqual(result.returncode, 0, result.stderr)
        for expected in ['namespace: workloads', 'serviceAccountName: external-manager',
                         'name: external-manager', 'fieldPath: metadata.namespace', 'enableServiceLinks: false',
                         '--cluster-domain=cluster.internal', '--installation-namespace=claims',
                         '--cloudflare-request-timeout=20s', '--max-ingress-rules=2000',
                         '--max-configuration-bytes=2097152', '--metrics-bind-address=:9090',
                         '--health-probe-bind-address=:9091']:
            self.assertIn(expected, result.stdout)
        self.assertNotIn('ServiceAccount', re.findall(r'^kind: (\S+)$', result.stdout, re.M))
        binding = next(doc for doc in result.stdout.split('---') if '\nkind: ClusterRoleBinding\n' in doc)
        self.assertIn('name: external-manager\n    namespace: workloads', binding)

    def test_old_values_and_packaged_defaults(self):
        # Old releases lack controller and image.digest values. Reuse must still
        # select this chart's own pin, not retain the old chart's release digest.
        result = self.render({"controller": None, "image": {"tag": "", "digest": None}})
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn('ghcr.io/cfgate/cfgate@' + DIGEST, result.stdout)
        self.assertIn('--cloudflare-request-timeout=30s', result.stdout)
        self.assertNotIn('--installation-namespace=', result.stdout)
        with tempfile.TemporaryDirectory() as directory:
            subprocess.run(["helm", "package", ".", "-d", directory], cwd=ROOT, check=True, capture_output=True)
            package, = Path(directory).glob('*.tgz')
            self.assertEqual(self.image(chart=str(package)), 'ghcr.io/cfgate/cfgate@' + DIGEST)

    def test_chart_pin_changes_and_missing_pin_rejects(self):
        with tempfile.TemporaryDirectory() as directory:
            chart = Path(directory)
            for name in ['Chart.yaml', 'values.yaml', 'values.schema.json']:
                shutil.copyfile(ROOT / name, chart / name)
            shutil.copytree(ROOT / 'templates', chart / 'templates')
            metadata = (chart / 'Chart.yaml').read_text()
            updated = 'sha256:' + 'b' * 64
            (chart / 'Chart.yaml').write_text(metadata.replace(DIGEST, updated))
            self.assertEqual(self.image({"image": {"tag": "", "digest": None}}, directory),
                             'ghcr.io/cfgate/cfgate@' + updated)
            for invalid in ['', 'sha256:bad']:
                (chart / 'Chart.yaml').write_text(metadata.replace(DIGEST, invalid))
                result = self.render(chart=directory)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn('digest', result.stderr)

    def test_external_crds_and_rbac(self):
        result = self.render({"installCRDs": False, "rbac": {"create": False}}, deployment=False)
        self.assertEqual(result.returncode, 0, result.stderr)
        for kind in ['CustomResourceDefinition', 'ClusterRole', 'ClusterRoleBinding']:
            self.assertNotIn('kind: ' + kind + '\n', result.stdout)

    def test_invalid_values_rejected(self):
        for values in [
            {"image": {"digest": "latest"}}, {"image": {"digest": "sha256:abc"}},
            {"controller": {"cloudflareRequestTimeoutSeconds": 0}},
            {"controller": {"cloudflareRequestTimeoutSeconds": 9223372037}},
            {"controller": {"maxIngressRules": 0}}, {"controller": {"maxConfigurationBytes": 0}},
            {"controller": {"clusterDomain": "bad_domain"}},
            {"controller": {"installationNamespace": "Bad"}},
        ]:
            with self.subTest(values=values):
                result = self.render(values)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn('schema', result.stderr.lower())


if __name__ == "__main__":
    unittest.main()
