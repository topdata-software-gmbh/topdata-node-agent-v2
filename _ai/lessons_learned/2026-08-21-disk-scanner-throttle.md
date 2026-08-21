# 2026-08-21 - Disk scanner throttle + shell process-management gotchas

## Context
The Go node-agent (`topdata-node-agent-v2`) tails Shopware logs and reports disk usage. After deploying to a prod server with 18 shops, the server became unstable / unreachable — suspected disk I/O pressure. We investigated via Prometheus (node-exporter) and reworked the disk-usage code, adding a throttled scanner and a `/disk-eaters` growth endpoint.

## Challenge
The original `internal/monitor/host_monitor.go` ran `exec du -sb` in a goroutine **per shop**, each sleeping exactly `1 * time.Hour` from startup. With 18 shops this fired **18 concurrent `du` processes in lockstep every hour**, each walking the full shop tree (vendor/, var/cache, media…) = a periodic disk-read storm. The log tailing was a red herring.

## Discovery/Solution
- **Confirm in Prometheus with `node_procs_blocked` and `rate(node_disk_io_time_seconds_total[5m])`** — spikes *exactly on the hour, every hour* prove a synchronized scan storm, and `node_procs_blocked` explains why the box is "unreachable" (processes stuck on I/O).
- **Throttle + desync, don't just lengthen:** replaced per-shop `du` with a single `DiskScanner` doing a pure-Go `filepath.WalkDir`, bounded by a **concurrency semaphore** (`disk.scan_concurrency`, default 1) and a **per-shop phase offset** (random 0..interval) so scans never realign. First scan runs promptly at boot (serialized by the semaphore), then each shop settles into its fixed phase.
- **mtime-skip "incremental" does NOT save I/O:** a change deep in the tree only bumps the *immediate parent* dir's mtime — ancestors are NOT updated. So you cannot skip unchanged subtrees without descending into every directory anyway. Accurate totals require stat-ing every inode; the real levers are interval + concurrency cap + optional `--exclude` of regenerable dirs (`var/cache`).
- **Growth comes for free** from the same walk: record per-dir size + scan timestamp in a state file; growth rate = Δsize/Δt. Ranked via a new `/disk-eaters` endpoint (Basic Auth, `?shop/&top=/&by=rate|size`, JSON or `text/plain`).
- **Config:** `disk.scan_interval` (6h), `disk.scan_concurrency` (1), `disk.exclude` (`var/cache`), `disk.growth_max_depth` (3), `disk.state_file`.

## Key Takeaways
- **Synchronized periodic work across many entities = storm.** Always jitter phases and cap concurrency; never `sleep(fixedInterval)` from a shared start time.
- **`du`/stat cost is driven by inode count, not file size** — cache/media dirs dominate. Exclude regenerable paths from both the metric and the I/O.
- **Directory mtime does not propagate upward** on deep changes → subtree-skip caching on mtime is unsound.
- **Smoke-test gotcha in this shell/tool env:** `pkill -f 'topdata-agent'` killed its *own* shell because the pattern matched the launching command line. Use the bracket trick `pkill -f '[t]opdata-agent'` and run it in a **separate command** from the launcher.
- **Never background a long-lived server inside a command that also does foreground work** (sleeps/curls): when the tool times out it kills the whole process group, taking the server down. Launch with `setsid bash -c '...' </dev/null >/dev/null 2>&1 & disown` in a short standalone command, then probe in separate commands.
- A brand-new deploy showing zero disk metrics for hours is a UX trap — do the **first scan at boot**, not after a full jitter delay.
