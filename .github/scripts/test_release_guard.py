import base64
import copy
import unittest
from unittest.mock import patch

import release_guard as guard


class ReleaseGuardTests(unittest.TestCase):
    def setUp(self):
        self.sha = "a" * 40
        self.operator_sha = "b" * 40
        self.digest = "sha256:" + "c" * 64
        self.responses = {
            ("git", "rev-parse", "HEAD"): self.sha,
            ("git", "rev-parse", "refs/tags/v1.5.0^{commit}"): self.sha,
            ("git", "ls-remote", "--tags", "origin", "refs/tags/v1.5.0", "refs/tags/v1.5.0^{}"): self.sha + "\trefs/tags/v1.5.0",
            ("helm", "show", "chart", "."): 'version: 1.5.0\nappVersion: "0.2.0-alpha.6"\nannotations:\n  cfgate.io/operator-image-digest: ' + self.digest,
            ("helm", "template", "cfgate", ".", "--show-only", "templates/deployment.yaml"): '          image: "ghcr.io/cfgate/cfgate@' + self.digest + '"',
            ("oras", "resolve", "ghcr.io/cfgate/cfgate:0.2.0-alpha.6"): self.digest,
            ("oras", "resolve", "ghcr.io/inherent-design/cloudflared:2026.9.3-h2c.1"): self.digest,
        }
        self.release_path = "repos/cfgate/cfgate/releases/tags/v0.2.0-alpha.6"
        self.source_path = f"repos/cfgate/cfgate/contents/internal/cloudflared/deployment.go?ref={self.operator_sha}"
        self.api_responses = {
            self.release_path: {"tag_name": "v0.2.0-alpha.6", "draft": False, "published_at": "2026-09-29T00:00:00Z", "prerelease": True},
            "repos/cfgate/cfgate/git/ref/tags/v0.2.0-alpha.6": {"object": {"type": "commit", "sha": self.operator_sha}},
            self.source_path: {"encoding": "base64", "content": base64.b64encode(b'DefaultImage = "ghcr.io/inherent-design/cloudflared:2026.9.3-h2c.1"').decode()},
            "repos/inherent-design/cloudflared/releases/tags/v2026.9.3-h2c.1": {"tag_name": "v2026.9.3-h2c.1", "draft": False, "published_at": "2026-09-29T00:00:00Z"},
        }
        self.attestation_error = False
        self.calls = []

    def command(self, *args):
        self.calls.append(args)
        if args[:3] == ("gh", "attestation", "verify"):
            if self.attestation_error:
                raise RuntimeError("attestation verification failed")
            self.assertIn(self.operator_sha, args)
            self.assertIn("--bundle-from-oci", args)
            self.assertIn("refs/tags/v0.2.0-alpha.6", args)
            self.assertIn("cfgate/cfgate/.github/workflows/release.yml", args)
            return "verified"
        return self.responses[args]

    def verify(self, tag="v1.5.0", sha=None, package=None):
        with patch.object(guard, "run", side_effect=self.command), patch.object(guard, "api", side_effect=lambda path: copy.deepcopy(self.api_responses[path])):
            return guard.verify(tag, sha or self.sha, package)

    def test_pin_and_render_must_match_verified_digest(self):
        metadata = ("helm", "show", "chart", ".")
        render = ("helm", "template", "cfgate", ".", "--show-only", "templates/deployment.yaml")
        for key, value in [
            (metadata, 'version: 1.5.0\nappVersion: 0.2.0-alpha.6'),
            (metadata, self.responses[metadata].replace(self.digest, "sha256:" + "d" * 64)),
            (render, '          image: "ghcr.io/cfgate/cfgate:0.2.0-alpha.6"'),
            (render, '          image: "ghcr.io/cfgate/cfgate@sha256:' + "d" * 64 + '"'),
        ]:
            original = self.responses[key]
            self.responses[key] = value
            with self.subTest(key=key, value=value), self.assertRaises(ValueError):
                self.verify()
            self.responses[key] = original

    def test_packaged_image_is_checked(self):
        package = "dist/cfgate-1.5.0.tgz"
        metadata = ("helm", "show", "chart", package)
        render = ("helm", "template", "cfgate", package, "--show-only", "templates/deployment.yaml")
        self.responses[metadata] = self.responses[("helm", "show", "chart", ".")]
        self.responses[render] = self.responses[("helm", "template", "cfgate", ".", "--show-only", "templates/deployment.yaml")]
        self.verify(package=package)
        self.responses[render] = '          image: "ghcr.io/cfgate/cfgate:unverified"'
        with self.assertRaisesRegex(ValueError, "default Deployment"):
            self.verify(package=package)

    def test_valid_prerelease_dependency(self):
        self.assertEqual(self.verify()["operator_source"], self.operator_sha)

    def test_annotated_tag(self):
        key = ("git", "ls-remote", "--tags", "origin", "refs/tags/v1.5.0", "refs/tags/v1.5.0^{}")
        self.responses[key] = "d" * 40 + "\trefs/tags/v1.5.0\n" + self.sha + "\trefs/tags/v1.5.0^{}"
        self.verify()

    def test_invalid_tags_reject_before_commands(self):
        for tag in ["v1.5.0\nclean=oops", "v1.5.0;touch /tmp/x", "--help", "1.5.0", "v01.5.0", "v1.5.0$(id)"]:
            with self.subTest(tag=tag), self.assertRaises(ValueError):
                self.verify(tag)
        self.assertEqual(self.calls, [])

    def test_checkout_and_tag_mismatch(self):
        for key in [("git", "rev-parse", "HEAD"), ("git", "rev-parse", "refs/tags/v1.5.0^{commit}"), ("git", "ls-remote", "--tags", "origin", "refs/tags/v1.5.0", "refs/tags/v1.5.0^{}")]:
            original = self.responses[key]
            self.responses[key] = "d" * 40 + ("\trefs/tags/v1.5.0" if "ls-remote" in key else "")
            with self.subTest(key=key), self.assertRaises(ValueError):
                self.verify()
            self.responses[key] = original

    def test_chart_versions_reject(self):
        for text in ['version: 1.4.0\nappVersion: 0.2.0-alpha.6', 'version: 1.5.0\nappVersion: latest', 'version: 1.5.0\nappVersion: "$(id)"']:
            self.responses[("helm", "show", "chart", ".")] = text
            with self.subTest(text=text), self.assertRaises(ValueError):
                self.verify()

    def test_unpublished_dependencies_reject(self):
        for path in [self.release_path, "repos/inherent-design/cloudflared/releases/tags/v2026.9.3-h2c.1"]:
            original = copy.deepcopy(self.api_responses[path])
            for key, value in [("draft", True), ("published_at", None), ("tag_name", "v9.9.9")]:
                self.api_responses[path] = {**original, key: value}
                with self.subTest(path=path, key=key), self.assertRaises(ValueError):
                    self.verify()
            self.api_responses[path] = original

    def test_missing_image_or_provenance_reject(self):
        for key in [key for key in self.responses if key[:2] == ("oras", "resolve")]:
            original = self.responses[key]
            self.responses[key] = "not-a-digest"
            with self.subTest(key=key), self.assertRaises(ValueError):
                self.verify()
            self.responses[key] = original
        self.attestation_error = True
        with self.assertRaises(RuntimeError):
            self.verify()

    def test_source_connector_required(self):
        self.api_responses[self.source_path]["content"] = base64.b64encode(b'DefaultImage = "cloudflare/cloudflared:latest"').decode()
        with self.assertRaises(ValueError):
            self.verify()


if __name__ == "__main__":
    unittest.main()
