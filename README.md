# topdata-node-agent

A self-sufficient Go-based monitoring agent for Shopware 6 instances. It replaces
the previous PHP-based `node-agent` and runs as a single binary with zero external
runtime dependencies.

## Architecture

- **Discovery**: scans `/srv/topdata-shops/prod-shops/` for active Shopware 6 shops
  (identified by a `var/log` directory). `retired-shops` is ignored entirely.
- **Log monitoring**: tails each shop's `var/log/prod-YYYY-MM-DD.log` in real-time
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

| Metric | Type | Description |
|---|---|---|
| `shopware_critical_errors_total` | Counter | Total number of critical errors in Shopware logs, labelled by `shop` |
| `shopware_shop_disk_usage_bytes` | Gauge | Disk usage of the shop directory in bytes, labelled by `shop` |

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