#!/bin/sh
# Generate release manifests to dist/.
# Env: IMAGE - controller image ref; when set, patches kustomization before build
set -eu

if [ -n "${IMAGE:-}" ]; then
	(cd config/manager && kustomize edit set image "controller=${IMAGE}")
fi

rm -rf dist
mkdir -p dist/crds
for f in config/crd/bases/cfgate.io_*.yaml; do
	name=$(basename "$f" .yaml)
	cp "$f" "dist/crds/${name#cfgate.io_}.yaml"
done
cat dist/crds/*.yaml >dist/crds.yaml
kubectl kustomize config/default >dist/install.yaml

if [ -n "${IMAGE:-}" ]; then
	git checkout config/manager/kustomization.yaml 2>/dev/null || true
fi

echo "Release manifests written to dist/"
ls -la dist/ dist/crds/
