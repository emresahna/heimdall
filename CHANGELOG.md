# Changelog

All notable changes to this project are documented in this file. Entries are prepared from
[Conventional Commits](https://www.conventionalcommits.org/) with `make changelog` and reviewed as
part of each release.

## [Unreleased]

## [0.1.0] - 2026-08-06

### Added

- Initial syscall-based HTTP agent, gRPC ingestion server, ClickHouse storage, and embedded UI.
- Kubernetes manifests, Docker Compose workflow, Helm chart, and kind smoke-test flow.
- BPF map-pressure metrics, circuit-breaker instrumentation, ClickHouse retries, database-aware
  health endpoints, CI quality gates, and project licensing.
- Release automation for versioned agent/server images and packaged Helm charts.
- Build-version reporting through both binaries and the server health endpoint.

### Changed

- Refined collector lifecycle, event parsing, configuration, and Docker build reproducibility.

### Fixed

- Corrected timestamp formatting and aligned local CGO build settings.
