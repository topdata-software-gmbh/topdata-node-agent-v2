# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Changed
- `shops.root` now points directly at the directory containing shop folders (no `prod-shops` suffix appended anymore). Default and deploy configs updated to `/srv/topdata-shops/prod-shops`; the real `deploy/vars/vault.yml` must be updated accordingly.
- Removed the hardcoded default Basic Auth credentials (`admin`/`fete`). `TOPDATA_AGENT_AUTH_USERNAME` and `TOPDATA_AGENT_AUTH_PASSWORD` are now required: `serve` exits with an error at startup if they are not set.

### Added
- `serve --shops-root` and `serve --listen-address` CLI flags to override the shops root directory and listen address (take precedence over `TOPDATA_AGENT_*` env vars when set).
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
