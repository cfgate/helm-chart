# Changelog

All notable changes to the cfgate Helm chart are documented in this file.


## [1.0.10] - 2026-02-19

### Features

- Add cosign keyless signing to chart releases

### Maintenance

- Bump appVersion to 0.1.0-alpha.12, chart version to 1.0.10

## [1.0.9] - 2026-02-19

### Maintenance

- Update artifacthub repositoryID

## [1.0.8] - 2026-02-19

### Features

- Initial commit
- Add Dockerfile for container builds
- Add docs, ci, mise tooling
- Alpha.3 implementation
- CloudflareDNS CRD (composable architecture)
- Add helm chart v1.0.0
- Helm chart v1.0.1
- Alpha.6 comprehensive stabilization (unreleased)
- **(controller)** Alpha.7 reconciler stabilization and HTTPRoute credential inheritance
- Scaffold Astro alongside Hono worker
- **(site)** Wire static assets and OG tags into Astro layout
- **(site)** Integrate brand design system, i18n, and Starwind UI
- **(site)** Add Hindi translation, fix zh locale label
- **(site)** Add "images"

### Bug Fixes

- Remove deprecated Connections field (SA1019)
- Add kustomize directory structure
- Use release version tag in install.yaml
- SA1019 events API migration + reconciliation bugs
- Alpha.5 controller stabilization
- **(controller)** Alpha.6 reconcile, deletion, and API stabilization
- **(controller)** Logging guard and em-dash removal
- **(controller)** Register v1beta1 scheme, sync AccessPolicy CRD, demote noisy log
- **(test)** Add fallback credentials to deletion invariant DNS resource
- Patch task scripts for empty-arg bug, fragile cd, contract docs
- **(site)** Update English subtitle to match translation structure
- **(ci)** Add missing latest tag to release

### Testing

- Alpha.3 E2E suite (85/85 passing)
- **(e2e)** Alpha.6 coverage expansion and 94/94 stabilization
- Add E2E invariant tests for structural property verification
- Fix invariant assertion, add conflict retry to bare Get/Update sites

### Documentation

- Update Gateway API version and consolidate test tasks
- Update README and examples for v0.1.0-alpha.1
- Alpha.3 samples and examples
- Godoc comments and logging audit
- Add git-cliff changelog and update project docs
- Add shields.io badges to README
- Fix CRD table, add credential resolution and troubleshooting
- Fix deployment names, remove dead dns-sync annotations from examples
- Alpha.9 documentation overhaul, purge origin-no-tls-verify, fix examples
- Convert ASCII diagrams to Mermaid

### Refactoring

- Extract shared task scripts for mise/CI invariance
- **(site)** Switch to published @inherent.design/brand package
- **(site)** Use brand components, theme button globally

### Infrastructure

- Initialize cfgate.io as wrangler
- Add version injection to builds
- Use kubectl kustomize
- Cfgate.io v0.1.1 with route fix
- Alpha.4 CI/CD improvements
- Cfgate.io v0.1.2 custom_domain for auto DNS

### CI/CD

- Separate e2e, drop mise
- Bump golangci-lint to v2.8.0
- Update workflows; remove e2e
- Add path filter to pull_request trigger
- Remove workflow_dispatch from release
- Use git-cliff for release notes generation
- Add Artifact Hub metadata and annotations

### Maintenance

- Clean CI workflows and dead code
- Pin doc2go version
- Organize mise.toml
- Fix cfgate.io bootstrap
- Local dev tasks + docs
- Local dev fixes (docker cache, mise tasks)
- Reset kustomization.yaml after local deploy
- Update changelog for alpha.6 and fix badge layout
- **(chart)** Bump to v1.0.3 / appVersion 0.1.0-alpha.7
- Update changelog
- Update pnpm-lock
- **(site)** Update packages
- **(site)** Remove stale scripts, rename deploy:cf -> deploy
- **(site)** Fix scripts
- Update brand package
- Update README
- Sync chart + app version
- **(site)** Update packages
- **(site)** Update images
- Rename Cloudflare worker
- Update workflows, bump versions

### Other

- Migrate workflows to Blacksmith
- Inherent-design/cfgate/charts/cfgate -> cfgate/helm-chart
