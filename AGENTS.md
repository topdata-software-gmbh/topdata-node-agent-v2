# AGENTS.md

Go (1.21+) monitoring agent for Shopware 6: tails daily shop logs, reports disk usage, exposes Prometheus `/metrics` on `:9144` with Basic Auth. Replaces a legacy PHP agent; runs as a single systemd service.

## Commands

```sh
go build -o bin/topdata-agent .   # binary; output dir /bin/ is gitignored
go build ./...                    # verification (no Makefile, no CI, no tests in repo)
go vet ./...
```

There are no test files — `go test` is empty. Smoke-test manually: run `serve` with `TOPDATA_AGENT_LISTEN_ADDRESS=:19144`, curl `/metrics` with auth, append a `[critical]` line to a synthetic shop log.

## Deployment (Ansible)

`deploy/` deploys the agent to all 10 shop-hosting servers (`deploy/hosts.ini`; arch picked per host via `ansible_facts.architecture`). `./deploy/deploy-to-prod.sh` cross-compiles both arches into `deploy/bin/` (gitignored) and runs the playbook with `--ask-vault-pass`. Secrets live in the ansible-vault file `deploy/vars/vault.yml` (gitignored; see `vault.example.yml`) with keys `auth_username`, `auth_password`, `shops_root`. The agent `log.Fatal`s at startup if the shops directory is missing, so the smoke test in the playbook will fail on a server where it does not exist yet. The real `vault.yml` must be edited (`ansible-vault edit`) to update `shops_root` after the `prod-shops` semantics change.

## Build gotcha: fsnotify `replace`

`go.mod` carries `replace gopkg.in/fsnotify.v1 => github.com/fsnotify/fsnotify v1.4.7`. It is **required**: the archived `hpcloud/tail` dependency fails to build without it (mismatched module path). Do not remove it when tidying. ADR notes `github.com/nxadm/tail` as the planned follow-up to drop it.

## Config (viper)

Keys are dotted and map to env with prefix `TOPDATA_AGENT_` (`cmd/serve.go:57-59`; replacer converts `.` to `_`):
- `shops.root` → `TOPDATA_AGENT_SHOPS_ROOT`, default `/srv/topdata-shops/prod-shops`
- `auth.username`/`auth.password` → `TOPDATA_AGENT_AUTH_USERNAME/PASSWORD`, **required** — no defaults; `serve` `log.Fatal`s at startup if either is unset. Production values come from the ansible-vault `deploy/vars/vault.yml`, rendered into the systemd `EnvironmentFile`
- `listen.address` → `TOPDATA_AGENT_LISTEN_ADDRESS`, default `:9144`

No config file support. `serve --shops-root <dir>` binds to `shops.root` (takes precedence over the env var when set).

## Architecture

- `cmd/` — Cobra CLI (`root.go` + `serve.go`). `serve` is the only subcommand.
- `internal/discovery` — `FindShops` scans the configured root **directly** (no `prod-shops` suffix appended), keeps dirs containing `vol/www/var/log`.
- `internal/monitor/log_monitor.go` — `TailLog` tails `vol/www/var/log/prod-YYYY-MM-DD.log` (hpcloud/tail, `Poll: true`). Counts lines matching `.CRITICAL:` or `[critical]` into `shopware_critical_errors_total{shop}`. A 30s watchdog restarts the tail at midnight for the new daily file — do not refactor back to a single blocking `range t.Lines`.
- `internal/monitor/host_monitor.go` — `UpdateDiskUsage` runs `du -sb` hourly → `shopware_disk_usage_bytes{shop}`. Requires `du` on PATH.
- `serve` starts one goroutine per shop and blocks in `http.ListenAndServe`; it `log.Fatal`s if discovery errors, and fails at startup if the configured shops directory doesn't exist.

Metrics are `promauto`-registered at package init (global registry); adding tests that run `serve` twice will panic on duplicate registration.

## Repo conventions

- `CHANGELOG.md` is maintained under `[Unreleased]` for new features.
- Architecture decisions go in `_ai/technical_decisions/` (ADR format); plans/reports live in `_ai/backlog/`. `.ctx.yaml` sets the control-plane `project_id: topdata-node-agent-v2`.