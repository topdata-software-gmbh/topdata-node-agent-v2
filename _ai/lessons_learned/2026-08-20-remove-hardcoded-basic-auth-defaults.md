# [2026-08-20] - Removing Hardcoded Basic Auth Defaults from the Go Agent

## Context

The topdata-node-agent-v2 (Go, Prometheus exporter) ships with `/metrics` secured by Basic Auth. The session started with a question about how credentials are stored, then moved into a security fix.

## Challenge

1. The agent shipped with **hardcoded default credentials** (`admin`/`fete`) as viper defaults — a security risk committed to git. Anyone who started the agent without env vars got a working, well-known credential.
2. When a user sets a strong password in the ansible-vault, the path from vault to the running agent was not obvious.

## Discovery/Solution

**Secret flow (vault → agent):**
1. `deploy/vars/vault.yml` (ansible-vault encrypted, gitignored) holds `auth_username`/`auth_password`.
2. `deploy-to-prod.sh` runs the playbook with `--ask-vault-pass` — vault is decrypted in memory only.
3. Playbook renders `templates/topdata-agent.env.j2` → `/etc/topdata-agent.env` on each host.
4. systemd unit declares `EnvironmentFile=/etc/topdata-agent.env`, exporting `TOPDATA_AGENT_AUTH_USERNAME/PASSWORD`.
5. viper (env prefix `TOPDATA_AGENT`, `.`→`_` replacer) exposes them as `auth.username`/`auth.password`, compared verbatim in `authMiddleware` against `r.BasicAuth()`.

**Fix applied:**
- Removed the `viper.SetDefault` lines for `auth.username`/`auth.password` from `cmd/serve.go` init().
- Added a startup guard at the top of `serve`'s Run func:
  ```go
  if !viper.IsSet("auth.username") || !viper.IsSet("auth.password") {
      log.Fatal("basic auth credentials not configured: set TOPDATA_AGENT_AUTH_USERNAME and TOPDATA_AGENT_AUTH_PASSWORD")
  }
  ```
- Verified with `go run . serve` → exits status 1 with the message.
- Updated README.md (defaults table → "(required)", curl example), AGENTS.md, CHANGELOG.md under `[Unreleased]`.

## Key Takeaways

- **Never ship default credentials** — prefer required-at-startup validation (`log.Fatal` if unset) over "defaults are intentional" (the old ADR/README rationale was wrong).
- **viper gotcha:** with `AutomaticEnv()`, `viper.IsSet(key)` returns `true` when the env var is *set but empty* — an empty password still passes the guard. If empty strings must be rejected, check `viper.GetString(...) == ""` too (flagged as follow-up in this session).
- **Ansible + systemd secret chain:** vault file → playbook-rendered `EnvironmentFile` → systemd env → `TOPDATA_AGENT_*` env vars → viper. Secrets never land in git; only the `.j2` template and `vault.example.yml` placeholders are committed.
- **Keep docs in sync:** removing defaults required touching README table, curl example, AGENTS.md config section, and CHANGELOG `[Unreleased]` — grep for the old values (`fete`, `admin`) to find all references.
- **Smoke test cheaply:** `go run . serve` (no env) is a 2-second check that the startup guard works; `go build ./... && go vet ./...` for compilation.