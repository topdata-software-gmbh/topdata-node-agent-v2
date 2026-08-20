# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- Initial release of the Go Node Agent.
- Migration of log monitoring and disk usage tracking from the PHP-based `node-agent`.
- Auto-discovery of active Shopware 6 shops in `/srv/topdata-shops/prod-shops/`.
- Real-time tailing of Shopware logs counting critical errors (`shopware_critical_errors_total`).
- Disk usage tracking per shop (`shopware_shop_disk_usage_bytes`).
- Prometheus-compatible `/metrics` endpoint secured with Basic Auth, configurable via environment variables.
- Cobra-based CLI with a single `serve` command.
