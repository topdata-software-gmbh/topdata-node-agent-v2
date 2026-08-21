# topdata-node-agent

A self-sufficient Go-based monitoring agent for Shopware 6 instances. It replaces
the previous PHP-based `node-agent` and runs as a single binary with zero external
runtime dependencies.

## Architecture

- **Discovery**: scans `shops.root` for active Shopware 6 shops (identified by a
  `vol/www/var/log` directory). Each matching subdirectory is treated as one shop.
- **Log monitoring**: tails each shop's `vol/www/var/log/prod-YYYY-MM-DD.log` in real-time
  and counts `[CRITICAL]` entries. The tail automatically restarts when the date
  changes to follow the new daily log file.
- **Host statistics**: periodically reports the disk usage of each shop directory.
- **Metrics endpoint**: exposes Prometheus-compatible metrics on port `9144`,
  protected with HTTP Basic Auth.

## Configuration

All settings are configurable via environment variables (prefix `TOPDATA_AGENT`),
with the following defaults:

| Env var | Default | Description |
|---|---|---|
| `TOPDATA_AGENT_SHOPS_ROOT` | `/srv/topdata-shops` | Root directory containing `prod-shops/` |
| `TOPDATA_AGENT_AUTH_USERNAME` | *(required)* | Basic Auth username for `/metrics` |
| `TOPDATA_AGENT_AUTH_PASSWORD` | *(required)* | Basic Auth password for `/metrics` |
| `TOPDATA_AGENT_LISTEN_ADDRESS` | `:9144` | Listen address of the metrics endpoint |

> `TOPDATA_AGENT_AUTH_USERNAME` and `TOPDATA_AGENT_AUTH_PASSWORD` are required —
> the agent refuses to start without them. Set them via the systemd
> `EnvironmentFile` (deployed by the Ansible playbook from the vault).

## Metrics

The agent registers the following metrics (via `prometheus/client_golang`, on the
global default registry). All metrics carry a `shop` label holding the shop's
directory name under `shops.root`.

| Metric | Type | Labels | Description |
|---|---|---|---|
| `shopware_critical_errors_total` | Counter | `shop` | Total number of critical errors found in the shop's `vol/www/var/log/prod-YYYY-MM-DD.log`. A line counts when it matches `.CRITICAL:` or `[critical]` (case-insensitive). Monotonic — it only increases for the lifetime of the process. |
| `shopware_shop_disk_usage_bytes` | Gauge | `shop` | Disk usage of the shop directory in bytes, measured with `du -sb` and refreshed hourly. Drops to the last value at rest; 0 if `du` is missing or fails. |

> The `shop` label value is the directory name discovered under `shops.root`
> (e.g. `muster-shop`), not the full path.

## Prometheus

Scrape target: `http://USERNAME:PASSWORD@host:9144/metrics`

Example `prometheus.yml` scrape config:

```yaml
scrape_configs:
  - job_name: topdata-agent
    metrics_path: /metrics
    static_configs:
      - targets:
          - arm1.srv.topinfra.de:9144
          - arm2.srv.topinfra.de:9144
          # ... all hosts from deploy/hosts.ini
    basic_auth:
      username: USERNAME
      password: PASSWORD
```

## Deployment (Ansible)

The agent is deployed to all shop-hosting servers via the playbook in `deploy/`.
Secrets live in an ansible-vault file at `deploy/vars/vault.yml` (gitignored).
Create it once, then edit it whenever the credentials or the shops root change.

### Creating the vault

```sh
ansible-vault create deploy/vars/vault.yml
```

Use the keys documented in `deploy/vars/vault.example.yml`:

```yaml
auth_username: topdata
auth_password: CHANGE_ME
shops_root: /srv/topdata-shops/prod-shops
```

### Editing the vault

Re-open the encrypted file with:

```sh
ansible-vault edit deploy/vars/vault.yml
```

### Other useful commands

```sh
ansible-vault view deploy/vars/vault.yml          # print decrypted contents
ansible-vault rekey deploy/vars/vault.yml         # change the vault password
```

### Deploying

`./deploy/deploy-to-prod.sh` cross-compiles both architectures into
`deploy/bin/` and runs the playbook with `--ask-vault-pass`, which prompts for
the vault password used to decrypt `deploy/vars/vault.yml` in memory.

```sh
./deploy/deploy-to-prod.sh
# or, against a single host:
ansible-playbook -i deploy/hosts.ini deploy/playbook-deploy.yaml \
  --ask-vault-pass --limit arm6
```

## Build & Run

```sh
go build -o bin/topdata-agent .
./bin/topdata-agent serve
```

## systemd Service

`/etc/systemd/system/topdata-agent.service`:

```ini
[Unit]
Description=Topdata Node Agent for Shopware Monitoring
After=network.target

[Service]
Type=simple
User=root
EnvironmentFile=/etc/topdata-agent.env
ExecStart=/usr/local/bin/topdata-agent serve
Restart=on-failure

[Install]
WantedBy=multi-user.target
```

```sh
sudo cp bin/topdata-agent /usr/local/bin/topdata-agent
sudo systemctl daemon-reload
sudo systemctl enable --now topdata-agent
```