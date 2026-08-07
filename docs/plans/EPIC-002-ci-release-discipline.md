# Implementation Plan — EPIC-002: CI & release discipline

Status: Implemented and operationally verified via a disposable `v0.1.0-rc.1` prerelease.

Full release path smoke-verified on 2026-08-07:

- Pushed a `v0.1.0-rc.1` SemVer prerelease tag from a disposable commit whose `helm/Chart.yaml`
  (`version: 0.1.0-rc.1`, `appVersion: "v0.1.0-rc.1"`) and `CHANGELOG.md` heading matched the tag.
  The tag-only `.github/workflows/release.yml` validated the metadata, built both binaries, verified
  `--version`, ran `helm lint`/`helm template` against the rendered tag, pushed `ghcr.io/emresahna/heimdall-{agent,server}:v0.1.0-rc.1` (no `latest` written), packaged `heimdall-0.1.0-rc.1.tgz`, and created a
  GitHub Release marked `prerelease` containing the chart archive and `SHA256SUMS`.
- Verified OCI labels (`org.opencontainers.image.version=v0.1.0-rc.1`, revision, source), released-binary
  `--version` output, live server `/healthz` body (`ok` / `version: v0.1.0-rc.1`, 200) and unchanged
  `/readyz`, packaged-chart lint/render/tag agreement, and a checksum match.
- Ran a kind rollback smoke test against the packaged chart: installed with the published `v0.1.0-rc.1`
  image tags, upgraded to `v0.1.0-rc.2`, then `helm rollback heimdall 1` and confirmed both agent and
  server pods returned to the explicit `v0.1.0-rc.1` tag with `/healthz` reporting that version.
- Cleaned up the kind release and PVC; returned `main` to stable `0.1.0` metadata. One maintenance
  note: the kind cluster nodes are `arm64` while this epic's images are `linux/amd64`; the smoke test
  relied on Docker/qemu emulation at runtime and the label/`--version`/health checks passed.

Deviations from the plan:

- Verification used a prerelease tag (`v0.1.0-rc.1`) rather than a first stable tag, per review guidance to
  exercise the full path on a disposable release before the first stable tag.
- The `dist/SHA256SUMS` file embeds the `dist/` path prefix for checksum entries; consumers should
  verify within a matching directory layout (or with a path override).
- No registry override beyond GHCR was exercised; GHCR remains the validated default.
Epic: [EPIC-002 — CI & release discipline](../BACKLOG.md#epic-002--ci--release-discipline)
Prompt: `docs/ai/prompts/03-implementation-planning.md`
Priority: P1 · Phase 0 (Hygiene) · Effort: M
Scope: plan only — no code changes in this document.

---

## 1. Goal

Make every released agent, server, and Helm chart traceable to one immutable Git tag, and make the
release process repeatable in GitHub Actions.

The delivered release contract will be:

1. A SemVer tag, `vMAJOR.MINOR.PATCH` with an optional prerelease suffix, is the sole release input.
   The workflow rejects every other tag.
2. Both binaries embed that exact tag at build time, print it with `--version`, and the server also
   includes it in its successful `/healthz` response without changing the endpoint's health status.
3. CI validates the Helm chart (`helm lint` and a deterministic `helm template`) in addition to the
   existing Go checks.
4. A tag-triggered workflow builds and pushes immutable agent and server image tags derived from the
   tag, packages a chart whose `version` is the tag without its `v` prefix and whose `appVersion` is
   the full tag, then publishes the chart archive and release notes to the GitHub Release.
5. `CHANGELOG.md` records released, Conventional-Commit-derived changes before the tag is created.

Verified current-state evidence:

- `.github/workflows/ci.yml` runs lint, vet, race tests, tidy, and unversioned Go builds only; no
  Helm installation or chart validation exists.
- `cmd/agent/main.go` and `cmd/server/main.go` have no version variable or CLI version path;
  `internal/server/http.go` returns only `ok` from `/healthz`.
- `helm/Chart.yaml` is `version: 0.1.0` / `appVersion: "1.16.0"`, while agent and server defaults in
  `helm/values.yaml` and raw Kubernetes manifests are mutable `latest` tags.
- The repository has no Git tags, `CHANGELOG.md`, release workflow, release configuration, or image
  publishing workflow. The configured origin is `github.com/emresahna/heimdall`.

## 2. Architecture and release contract

This epic adds a small build-metadata boundary and two independent workflows. It deliberately does
not add image signing, a Helm repository, or multi-architecture builds.

```text
release commit (Chart metadata + CHANGELOG)
                 |
           git tag vX.Y.Z
                 |
        GitHub release workflow
           |                 |
  ldflags inject vX.Y.Z   Helm package overrides/checks
           |                 |
  agent/server images     heimdall-X.Y.Z.tgz + GitHub Release
           |
  --version; server /healthz reports vX.Y.Z
```

### Version source of truth

- A release starts only after a maintainer updates `helm/Chart.yaml` so `version: X.Y.Z` and
  `appVersion: "vX.Y.Z"`, updates `CHANGELOG.md`, merges those changes, and tags that exact commit
  `vX.Y.Z`.
- The release workflow validates the tag against `helm/Chart.yaml` before publishing. This prevents
  a chart/image mismatch rather than attempting to repair it in CI.
- Local, branch, and PR builds inject `dev`; this is the default value in source as well. A release
  build injects the exact `GITHUB_REF_NAME` through `-ldflags "-X main.version=<tag>"` for each
  command package.
- The agent and server expose the same value through `--version`; they log it at startup. The server
  returns a machine-readable, backward-compatible text health body such as `ok\nversion: vX.Y.Z\n`.
  It remains `200`/`503` based solely on the existing health checker. The agent has no health route,
  so its CLI/startup log are its version-reporting surfaces.

### Images and registry configuration

- The release workflow defaults to GitHub Container Registry:
  `ghcr.io/<repository-owner>/heimdall-agent` and `.../heimdall-server`.
- Repository Actions variables `CONTAINER_REGISTRY` and `IMAGE_NAMESPACE` override the registry host
  and namespace. Publishing to a non-GHCR registry is therefore an explicit repository configuration
  choice; credentials must be supplied as registry-specific repository secrets.
- Each release pushes only immutable, tag-derived image references (`vX.Y.Z`). It must not publish
  `latest`. Docker labels record the revision and version using OCI standard labels.
- Helm defaults become release-safe: agent/server repository values point to the published namespace,
  and the default tag is empty so templates fall back to `.Chart.AppVersion`. `latest` remains only
  for the unrelated third-party ClickHouse development default, documented as outside this epic.
  Raw Kubernetes manifests receive the same explicit release image repository/tag convention and a
  documented edit/overlay step, so they cannot silently pull a moving Heimdall image.

### Workflows and artifacts

| Workflow | Trigger | Responsibilities | Permissions |
|---|---|---|---|
| `ci.yml` | PRs and pushes to `main` | Existing Go checks; install pinned Helm; `helm lint helm`; render with `helm template` using a non-empty release name; fail if rendering has an empty or `:latest` Heimdall image reference | `contents: read` |
| `release.yml` | pushed `v*` tags (with optional protected manual retry) | Verify tag/Chart/CHANGELOG consistency; build and smoke-test both versioned binaries; build/push two images; package chart; create GitHub Release with generated notes and chart artifact | `contents: write`, `packages: write` |

The release workflow uses immutable action references/major versions already established by CI,
`docker/login-action`, `docker/build-push-action`, `docker/metadata-action`, and the GitHub CLI or
release action. Its job is gated before registry writes by all validation steps. It uses the
repository `GITHUB_TOKEN` for GHCR/GitHub Releases; an override registry receives its credentials
only via masked Actions secrets.

### Changelog process

- Add a Keep-a-Changelog-style `CHANGELOG.md` with an `Unreleased` section and a first historical
  release section generated from the existing Conventional Commit history.
- Add a checked-in changelog generator configuration (for example, `cliff.toml`) and a documented
  `make changelog` target that deterministically groups `feat`, `fix`, `perf`, `refactor`, and other
  Conventional Commit types. The target changes files only when a maintainer intentionally prepares
  a release.
- The release workflow verifies that the matching release heading exists and uses that section for
  GitHub Release notes. It does not commit to the tagged branch, avoiding a post-tag artifact whose
  changelog disagrees with the tag.

## 3. Affected Files

**Build metadata and binaries**

- `cmd/agent/main.go`
  - Define `version = "dev"`, make it overrideable with `-ldflags -X main.version=...`, and add a
    `--version` execution path before collector/network initialization.
  - Include the version in the normal startup log.
- `cmd/server/main.go`
  - Apply the identical default, linker injection point, `--version` path, and startup log.
  - Pass the immutable build version to the HTTP server constructor.
- `internal/server/http.go`
  - Store the supplied version in `HttpServer`; include it in the healthy health body while retaining
    existing `200`/`503` health semantics and existing API routes.
- `internal/server/http_test.go`
  - Extend health tests to assert the expected version-body contract and preserve unhealthy
    behavior.

**Build and local developer commands**

- `Makefile`
  - Add a `VERSION ?= dev` variable and use it for both local binary builds and optional local image
    tags; add `helm-lint`, `helm-template`, and `changelog` targets.
  - Keep generated BPF artifacts out of the release path; normal builds continue using committed
    artifacts.
- `Dockerfile.agent`, `Dockerfile.server`
  - Accept a build argument for version and pass it to their respective `go build -ldflags` commands.
  - Add OCI image labels for version and source revision. Do not change runtime privilege or base-image
    policy in this epic.

**Packaging and deployment**

- `helm/Chart.yaml`
  - Establish the first release-aligned semantic chart/app versions; later releases update both as
    part of the pre-tag commit.
- `helm/values.yaml`, `helm/templates/agent-ds.yaml`, `helm/templates/server-deploy.yaml`
  - Use `.Chart.AppVersion` when no explicit agent/server tag is supplied; document override behavior
    and eliminate `latest` as the Heimdall default.
- `deploy/k8s/agent-ds.yaml`, `deploy/k8s/server-deployment.yaml`
  - Replace mutable Heimdall image tags with the documented current release tag/repository, and add
    comments directing upgrades through an explicit version edit or a release-specific overlay.
  - Do not pin the third-party ClickHouse image in this epic; that needs an independent compatibility
    decision.

**CI, release, and documentation**

- `.github/workflows/ci.yml` — add pinned Helm setup, lint, and deterministic render checks while
  preserving existing Go gates.
- `.github/workflows/release.yml` (new) — tag validation, versioned build/image/chart publication,
  and GitHub Release creation.
- `CHANGELOG.md`, changelog generator configuration (new) — conventional-commit release notes and
  maintainable generation rules.
- `README.md` — add a concise release process: prerelease checks, chart/version update, changelog
  generation/review, tag push, published image names, Helm install/upgrade with an explicit version,
  version inspection, and rollback command.
- `docs/plans/EPIC-002-ci-release-discipline.md` — update status and record deviations/verification;
  retain this planning rationale.

## 4. Risks and decisions

| Risk | Likelihood | Mitigation |
|---|---|---|
| Image, binary, and chart versions drift | Medium | Require exact tag ↔ `Chart.yaml` validation before any push; derive Docker build args and image tags only from `GITHUB_REF_NAME`; template test checks the rendered image tag. |
| A tag is pushed before its changelog/Chart update | Medium | Protected release branch/review process; release workflow fails before publishing when version heading or chart metadata is absent/mismatched. A corrected tag requires a new semver tag, never force-moving the original. |
| `-ldflags` injection breaks because values contain shell syntax | Low | Accept only semver tag characters; use an explicit `VERSION` build arg and test `--version` from the produced Linux binaries. |
| GHCR works but a future external registry does not | Medium | Keep GHCR as the supported default; make hostname/namespace configurable and document required secrets. Validate configuration before Docker login/push. |
| Helm tooling/version differs locally from CI | Low | Pin Helm version in CI and document it; chart lint/render are the authoritative gates. |
| Changing `/healthz` body breaks an undocumented text consumer | Low | Preserve status code and an `ok` first line; add version as an additional line. Kubernetes probes inspect only HTTP success. |
| Generated changelog misclassifies a malformed commit | Medium | Keep the generated result reviewed in the release PR; treat Conventional Commit format as a release requirement rather than silently inventing categories. |
| Registry/package publishing is irreversible for a public tag | Medium | Run all validation and binary/chart smoke tests before the first push; publish only immutable tags; rollback uses a new corrective release or deployment rollback, not tag mutation. |

## 5. Milestones

**M1 — Reproducible version surfaces (~0.75d)**

- Add `dev` defaults and linker injection to both commands.
- Implement `--version`, startup logs, and server health version response.
- Thread version into `HttpServer` with focused HTTP tests.
- Add `VERSION` support to Makefile and Docker builds; locally verify `dev` and a representative
  `v0.1.0` injection.

**M2 — Helm/package integrity in CI (~0.5d)**

- Define release-safe Helm image defaults and raw-manifest tag convention.
- Add `helm lint` and deterministic `helm template` checks to CI, including assertions that Heimdall
  images resolve to explicit non-`latest` release tags.
- Ensure lint/render exercises both default enabled components and ClickHouse-disabled external-server
  configuration.

**M3 — Tag release workflow (~1d)**

- Add the tag-only workflow, semver/Chart/changelog validation, GHCR-default login, metadata, and
  multi-image builds/pushes.
- Package `helm/` with checked metadata, attach the `.tgz`, checksums, and generated release notes to
  a GitHub Release.
- Add a dry-run-compatible local/repository dispatch path that performs all validation without pushing
  (or use a separate non-publishing workflow) so credentials and metadata can be verified safely.

**M4 — Changelog and operator docs (~0.5d)**

- Add generated historical `CHANGELOG.md`, generator config, and Makefile target.
- Document release preparation, image/tag selection, version inspection, Helm upgrade, and rollback.
- Cut a first test release from a disposable prerelease tag, then validate all release artifacts before
  declaring the workflow ready for the first stable tag.

## 6. Validation Plan

**Automated, required on every PR/main push**

1. Existing gates: `golangci-lint run`, `go vet ./...`, `go test -race ./...`, `go mod tidy` with a
   clean diff, and `CGO_ENABLED=0 go build` for both commands.
2. `make VERSION=dev build`; execute both binary `--version` paths and assert `dev` is printed.
3. Focused HTTP tests prove healthy `/healthz` includes the injected version and an unhealthy checker
   still yields `503` rather than exposing a false healthy version.
4. `helm lint helm` and `helm template heimdall helm --namespace default`; fail if templating fails,
   any Helm-rendered agent/server image has `:latest`, or the default tag does not equal `appVersion`.
5. Render with `clickhouse.enabled=false` and an explicit external ClickHouse address to protect both
   template branches.

**Release-candidate/tag validation**

1. On a temporary `vX.Y.Z-rc.N` tag, verify workflow rejects/accepts only the documented prerelease
   syntax and confirms its Chart/app version agreement.
2. Inspect `agent --version` and `server --version` from the exact release-built images; query server
   `/healthz` and confirm the same tag appears while `/readyz` semantics remain unchanged.
3. Pull `ghcr.io/<owner>/heimdall-agent:vX.Y.Z` and `...server:vX.Y.Z`; inspect OCI labels and image
   digests. Confirm no `latest` was written by this release.
4. Run `helm lint` on the downloaded `.tgz`, then `helm template` and `helm upgrade --install --dry-run`
   with its default values. Assert rendered image refs match the tag exactly.
5. Verify the GitHub Release contains the chart archive, checksum, and the same release section found
   in `CHANGELOG.md`.

**Manual deployment smoke test**

1. Install the packaged chart into kind using the published agent/server image tags.
2. Confirm deployed pod image IDs point to the expected tag/digest and server `/healthz` exposes that
   tag.
3. Upgrade to a newer test tag, then execute the documented `helm rollback`; confirm both workloads
   return to the former explicit tag and binary version.

No performance benchmark is required: this epic changes build and operational metadata, not the
capture or ingestion hot paths.

## 7. Rollback Plan

- **Before publication:** fail the workflow before image/chart pushes for version, changelog, binary,
  or Helm validation failures. Correct the release commit and create a new tag; do not move a tag.
- **Deployed workloads:** `helm rollback <release> <revision>` restores the prior chart and its pinned
  agent/server image tags. Raw-manifest users reapply the prior committed manifest or explicitly set
  the earlier documented tag.
- **Published artifacts:** retain immutable published tags and GitHub Release assets for auditability.
  If a release is defective, mark its GitHub Release as pre-release/draft or add a clear deprecation
  notice, then publish a corrective version. Do not overwrite or delete a tag that may be deployed.
- **Code rollback:** revert the release-discipline commit(s) on `main` only if the workflow itself is
  faulty; the prior builds remain deployable. No data, schema, proto, or BPF migration is involved.
- **Feature flag:** none. Version embedding and pinned references are safe, observable release
  metadata; compatibility is maintained through the `ok` health-body first line and normal endpoint
  status codes.

## 8. Acceptance Criteria

Final checklist (mirrors [docs/BACKLOG.md EPIC-002](../BACKLOG.md)):

- [x] Agent and server binaries report their exact build version through `--version`; normal startup
  logs include it.
- [x] Server `/healthz` returns the embedded version in a successful health response without changing
  its existing healthy/unhealthy status semantics.
- [x] `-ldflags -X main.version` injects the release tag for both commands; local and CI defaults are
  explicitly `dev`.
- [x] CI runs pinned `helm lint` and deterministic `helm template` checks in addition to all existing
  Go checks.
- [x] Helm agent/server defaults render immutable release tags from `Chart.AppVersion`; no Heimdall
  deployment default uses `latest`.
- [x] A semver Git tag validates matching Chart/changelog metadata, produces two versioned images and
  a versioned packaged chart, and publishes a GitHub Release with the chart artifact and notes.
- [x] Registry hostname/namespace are configurable through documented GitHub Actions configuration;
  GHCR is the working default.
- [x] `CHANGELOG.md` exists, is generated/reviewed from Conventional Commits, and its matching release
  section supplies the GitHub Release notes.
- [x] README documents release, upgrade, version inspection, and rollback procedures.
- [x] A disposable tag and kind deployment/rollback smoke test have verified the full release path.

## 9. Out of Scope / Follow-ups

- Cosign/SLSA provenance, SBOMs, vulnerability scanning, and image signing (deferred full release
  hardening).
- Helm repository hosting / OCI chart publishing; this epic attaches a versioned chart archive to the
  GitHub Release only.
- Multi-architecture agent images and a kernel compatibility matrix.
- Pinning and lifecycle policy for third-party ClickHouse images.
- Automated semantic version selection, automatic tag creation, and GitHub Release auto-commit flows.
- Product-direction-dependent registry/distribution work called out in ADR-001(c).
