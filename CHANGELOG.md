# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [v1.0.1] - 2026-08-21

### Changed
- `shops.root` now points directly at the directory containing shop folders (no `prod-shops` suffix appended anymore). Default and deploy configs updated to `/srv/topdata-shops/prod-shops`; the real `deploy/vars/vault.yml` must be updated accordingly.
- Removed the hardcoded default Basic Auth credentials (`admin`/`fete`). `TOPDATA_AGENT_AUTH_USERNAME` and `TOPDATA_AGENT_AUTH_PASSWORD` are now required: `serve` exits with an error at startup if they are not set.
- Replaced the per-shop hourly `du -sb` goroutine (which fired 18 concurrent `du` processes on the hour and saturated disk I/O) with a single `DiskScanner`: a pure-Go `WalkDir` per shop, throttled by a concurrency semaphore (default 1) and de-synchronized via a per-shop phase offset. This bounds disk pressure regardless of shop count. The `shopware_shop_disk_usage_bytes` gauge now excludes configured directories (default `var/cache`).

### Added
- Unauthenticated `/healthz` liveness endpoint: returns `200 OK` with body `OK` whenever the process is listening (no Basic Auth). Used by the Ansible deploy smoke test instead of the auth-gated `/metrics`.
- Authenticated `/info` endpoint (Basic Auth, same middleware as `/metrics`): reports `version`, `uptime`, `started_at`, `listen_address`, `shops_root`, and `shops_total`. Supports `?format=json|text|markdown` and follows the `Accept` header (`text/plain`/`text/markdown`) when no `format` is given, mirroring `/disk-eaters`.
- Config keys (env `TOPDATA_AGENT_DISK_*`): `scan_interval` (default `6h`), `scan_concurrency` (default `1`), `exclude` (default `var/cache`, comma-separated), `growth_max_depth` (default `3`), `state_file` (default `/var/lib/topdata-agent/disk-state.json`).
- New `/disk-eaters` endpoint (Basic Auth, same middleware as `/metrics`): ranks directories by disk-growth rate across scans. Supports `?shop=`, `?top=`, `?by=rate|size`, `?format=json|text|markdown` and returns JSON, a `text/plain` ASCII table, or a `text/markdown` table (sizes human-readable). Defaults to JSON, or follows the `Accept` header (`text/plain`/`text/markdown`) when no `format` is given — so the listing can be viewed directly in a browser via `?format=text` or `?format=markdown`. Driven by the same periodic walk that feeds the disk-usage gauge.
- `serve --shops-root` and `serve --listen-address` CLI flags to override the shops root directory and listen address (take precedence over `TOPDATA_AGENT_*` env vars when set).
- SemVer release tooling: `scripts/deploy/deploy-next-version.sh` (modeled on `tradeguard-app`) presents an arrow-key Patch/Minor/Major menu, rotates `CHANGELOG.md`, creates an annotated git tag, pushes it, and runs `deploy/deploy-to-prod.sh` to build + deploy that exact version. The deployed binary's `--version` is now derived from `git describe --tags --always` (traceable SemVer) instead of the bare remote/commit.
- `deploy-to-prod.sh` now supports `-h|--help` (with a `usage()` function and doc header), `--build-only` (cross-compile only), `--deploy-only` (skip build, run ansible-playbook only), and passes any other argument (e.g. `--limit arm1`, `--check`, `-vvv`) through to ansible-playbook unchanged for single-server testing.
- Startup logging: prints agent version, shops root, discovered shops, and listen address.
- `--version` flag on the root command (version injectable via `-ldflags "-X github.com/topdata/node-agent/cmd.version=..."`).
- Ansible fleet deployment (`deploy/`): inventory for all 10 shop-hosting servers, per-host playbook (binary copy, env file, systemd unit, smoke test), and `deploy-to-prod.sh` for cross-compiling both architectures.
- Initial release of the Go Node Agent.
- Migration of log monitoring and disk usage tracking from the PHP-based `node-agent`.
- Auto-discovery of active Shopware 6 shops in `/srv/topdata-shops/prod-shops/`.
- Real-time tailing of Shopware logs counting critical errors (`shopware_critical_errors_total`).
- Disk usage tracking per shop (`shopware_shop_disk_usage_bytes`).
- Prometheus-compatible `/metrics` endpoint secured with Basic Auth, configurable via environment variables.
- Cobra-based CLI with a single `serve` command.
## [Unreleased]

## [Unreleased]

### Changed
- `/disk-eaters` now reports each directory's `SIZE` and `GROWTH/h` as a **residual**: the value minus its single biggest direct child (by size). Ancestors no longer echo their big children — they show only the unexplained remainder, and read `0` growth when all their growth is attributable to a child entry. The ranking is also deterministically stable on ties (growth → size, size → growth).

### Added
- `/critical-errors` endpoint (Basic Auth, same middleware as `/metrics`): reports the most recent critical Shopware error lines per shop from an in-memory ring buffer (100 lines/shop, full untruncated messages). Supports `?shop=<name>`, `?limit=N` (default 20, capped at 100) and `?format=json|text|markdown` with the usual `Accept`-header fallback. Entries carry timestamps and survive midnight log rotation; history resets on agent restart (the payload includes `agent_started_at`). Removed shops are purged together with their Prometheus series.
- First unit tests (`internal/monitor/error_buffer_test.go`) covering buffer eviction, snapshots and endpoint rendering.
- `discovery.interval` (env `TOPDATA_AGENT_DISCOVERY_INTERVAL`, default `15m`): the agent now re-discovers shops periodically. Added shops are monitored automatically; removed shops are stopped and their Prometheus series removed — no service restart required. `/info`'s `shops_total` is now live.

