# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.1.0] - Unreleased

### Security
- Updated Go toolchain to Go 1.26.11 in `go.mod` and Dockerfile builder (`golang:1.26-alpine`) to resolve 19 standard library vulnerabilities identified by `govulncheck` (2026-11-19).
- Updated production runtime container image to supported `alpine:3.24` (2026-11-19).
- Replaced `gitleaks-action` with verified standalone pinned `gitleaks` binary (v8.30.1) validating against published SHA256 checksums, eliminating organization licensing requirements and secrets in CI (2026-11-19).
- Hardened `pr-guard` workflow to pass `GITHUB_TOKEN` strictly via environment variables, avoiding command-line exposure (2026-11-19).
- Pinned third-party GitHub Actions to immutable full commit SHAs in CI and Docker workflows (2026-11-19).

### Added
- Sequential measurement scheduler periodically monitoring enabled Stellar payment corridors (`internal/scheduler`).
- Stellar Horizon pathfinding client querying `/paths/strict-send` to evaluate execution routes and received amounts (`internal/measure`).
- Dynamic trade sizing dynamically scaling sell amounts from corridor `target_usd_value` using sell-currency USD exchange rates (`internal/measure`).
- Independent mid-market FX reference rate fetcher querying Frankfurter API v2 (`internal/reference`).
- Loss percentage computation benchmarking Horizon execution against mid-market FX rates (`internal/measure`).
- Three-tier corridor integrity classification (`usable`, `degraded`, `unusable`) based on configurable loss thresholds (`internal/measure`).
- Per-leg Stellar asset verification tracking anchor domain, metadata presence, and verified status independently per asset leg (`internal/store`, `corridors.yaml`).
- PostgreSQL persistence layer managing `corridors`, `measurements`, and `state_change_events` (`internal/store`, `schema.sql`).
- State-change detection comparing against preceding corridor measurements to prevent redundant notifications (`internal/scheduler`).
- Plain HTTP POST webhook dispatcher delivering alert payloads on corridor state transitions (`internal/webhook`).
- REST API server (`internal/api`) exposing:
  - `GET /healthz` health check with database ping probe.
  - `GET /corridors` listing enabled corridors and metadata.
  - `GET /corridors/{id}/history` returning paginated corridor measurement records.
- Automated startup seeding synchronizing corridor configurations from `corridors.yaml` into PostgreSQL (`internal/seed`).
- Multi-stage `Dockerfile` and `docker-compose.yml` configuration for containerized deployment.
- Environment variable configuration loader with defaults (`internal/config`).
