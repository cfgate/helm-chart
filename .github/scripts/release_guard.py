"""Verify immutable chart source and published, attested operator dependencies."""

import argparse
import base64
import json
import os
from pathlib import Path
import re
import subprocess


VERSION = r"(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)(?:-[0-9A-Za-z]+(?:[.-][0-9A-Za-z]+)*)?"


def require(condition, message):
    if not condition:
        raise ValueError(message)


def run(*args):
    return subprocess.check_output(args, text=True).strip()


def api(path):
    return json.loads(run("gh", "api", path))


def scalar(chart, key):
    values = re.findall(r"^" + key + r":\s*['\"]?([^'\"\s]+)['\"]?\s*$", chart, re.M)
    require(len(values) == 1, f"expected one normalized {key}")
    require(re.fullmatch(VERSION, values[0]), f"invalid {key}")
    return values[0]


def published_release(repository, tag):
    release = api(f"repos/{repository}/releases/tags/{tag}")
    require(release.get("tag_name") == tag and release.get("draft") is False
            and release.get("published_at"), f"{repository} {tag} is not published")
    return release


def tag_commit(repository, tag):
    obj = api(f"repos/{repository}/git/ref/tags/{tag}")["object"]
    for _ in range(5):
        if obj["type"] == "commit":
            require(re.fullmatch(r"[a-f0-9]{40}", obj["sha"]), "invalid source SHA")
            return obj["sha"]
        require(obj["type"] == "tag", "release tag does not resolve to a commit")
        obj = api(f"repos/{repository}/git/tags/{obj['sha']}")["object"]
    raise ValueError("release tag nesting limit exceeded")


def image_digest(image):
    digest = run("oras", "resolve", image)
    require(re.fullmatch(r"sha256:[a-f0-9]{64}", digest), "invalid image digest")
    return digest


def verify_image_pin(chart, digest):
    metadata = run("helm", "show", "chart", chart)
    pins = re.findall(r'^  cfgate\.io/operator-image-digest:\s*[\'"]?(sha256:[a-f0-9]{64})[\'"]?\s*$', metadata, re.M)
    require(pins == [digest], "chart operator pin does not match verified release digest")
    deployment = run("helm", "template", "cfgate", chart, "--show-only", "templates/deployment.yaml")
    images = re.findall(r'^\s+image:\s*[\'"]?([^\s\'"]+)[\'"]?\s*$', deployment, re.M)
    require(images == ["ghcr.io/cfgate/cfgate@" + digest],
            "default Deployment must install the verified operator digest")


def verify(tag, sha, package=None):
    require(re.fullmatch("v" + VERSION, tag), "invalid chart release tag")
    require(re.fullmatch(r"[a-f0-9]{40}", sha), "invalid chart source SHA")
    require(run("git", "rev-parse", "HEAD") == sha, "checkout does not match event SHA")
    require(run("git", "rev-parse", f"refs/tags/{tag}^{{commit}}") == sha,
            "chart tag does not match event SHA")
    remote = run("git", "ls-remote", "--tags", "origin", f"refs/tags/{tag}", f"refs/tags/{tag}^{{}}")
    refs = dict(line.split()[::-1] for line in remote.splitlines())
    require(refs.get(f"refs/tags/{tag}^{{}}", refs.get(f"refs/tags/{tag}")) == sha,
            "remote chart tag moved or disappeared")
    metadata = run("helm", "show", "chart", ".")
    version, app = scalar(metadata, "version"), scalar(metadata, "appVersion")
    require(tag == "v" + version, "Chart.yaml version does not match tag")
    published_release("cfgate/cfgate", "v" + app)
    operator_sha = tag_commit("cfgate/cfgate", "v" + app)
    image = "ghcr.io/cfgate/cfgate:" + app
    digest = image_digest(image)
    verify_image_pin(".", digest)
    if package is not None:
        verify_image_pin(package, digest)
    # These checks bind the available OCI digest to the released source, rather
    # than trusting the mutable image tag or the release's target_commitish.
    run("gh", "attestation", "verify", "oci://ghcr.io/cfgate/cfgate@" + digest,
        "--bundle-from-oci",
        "--repo", "cfgate/cfgate", "--signer-workflow", "cfgate/cfgate/.github/workflows/release.yml",
        "--source-ref", "refs/tags/v" + app, "--source-digest", operator_sha)
    source = api(f"repos/cfgate/cfgate/contents/internal/cloudflared/deployment.go?ref={operator_sha}")
    require(source.get("encoding") == "base64", "unexpected source encoding")
    code = base64.b64decode(source["content"]).decode()
    matches = re.findall(r'DefaultImage\s*=\s*"(ghcr\.io/inherent-design/cloudflared:([0-9A-Za-z.-]+))"', code)
    require(len(matches) == 1, "released operator must identify one versioned connector")
    fork_image, fork_version = matches[0]
    require(re.fullmatch(VERSION, fork_version), "invalid connector version")
    published_release("inherent-design/cloudflared", "v" + fork_version)
    fork_digest = image_digest(fork_image)
    return {"version": version, "source": sha, "app_version": app,
            "operator_source": operator_sha, "operator_image": image, "operator_digest": digest,
            "connector_image": fork_image, "connector_digest": fork_digest}


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--package", help="also verify the packaged chart's default image")
    args = parser.parse_args()
    evidence = verify(os.environ["RELEASE_TAG"], os.environ["RELEASE_SHA"], args.package)
    Path("dist").mkdir(exist_ok=True)
    Path("dist/release-dependencies.json").write_text(json.dumps(evidence, indent=2) + "\n")
    with open(os.environ["GITHUB_OUTPUT"], "a") as output:
        output.write("clean=" + evidence["version"] + "\n")
    print(json.dumps(evidence, indent=2))
