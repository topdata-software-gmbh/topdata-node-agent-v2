---
title: "ADR: Compress topdata-agent binary for faster fleet transfer"
status: decided
date: 2026-08-21
decision: >
  Strip the Go binary at build time with `-s -w` and transfer it via
  `ansible.posix.synchronize` using zstd-compressed rsync (`--zc=zstd`,
  level 3) instead of the uncompressed `ansible.builtin.copy` module.
  Targets must run rsync >= 3.2.0 (with zstd); the playbook fails fast otherwise.
context: >
  The deploy pipeline (`deploy/deploy-to-prod.sh` + `deploy/playbook-deploy.yaml`)
  cross-compiles two binaries into deploy/bin/ and copies them to 10 fleet
  servers over SSH. The primary driver is faster transfers. The original build
  used only `-X version=...` (no strip) and `ansible.builtin.copy`, which sends
  the full binary uncompressed. Measured: stripping cuts the binary ~33%
  (18.4 MB -> 12.4 MB). rsync with zstd compresses the wire stream far better
  than the plain copy/SFTP channel.
options:
  - id: strip_rsync_zstd
    text: >
      Add `-s -w` to build ldflags; switch copy -> ansible.posix.synchronize with
      compress_choice=zstd, compress_level=3, compress=yes. Hard-require rsync>=3.2.0.
  - id: strip_ssh_c
    text: Keep copy module but enable SSH-level `-C` compression. No new deps, but uploads the full file each time with weaker zlib.
  - id: upx
    text: UPX-pack the binary. Shrinks on-disk size but adds runtime decompression; not a transfer-speed win beyond what strip already gives.
consequences:
  - Binary is ~33% smaller; transfer is zstd-compressed over SSH.
  - Panic stack traces lose symbol/line info (debug build can be made on demand).
  - Controller needs `ansible.posix` collection; targets need rsync >= 3.2.0 with zstd.
  - Deploy fails fast with a clear message if a target's rsync is too old.
alternatives:
  - Plain rsync `-z` (zlib) is universally supported but slower/slightly worse ratio than zstd.
  - UPX rejected: no transfer benefit beyond strip, adds runtime RAM cost.
related:
  - deploy/deploy-to-prod.sh
  - deploy/playbook-deploy.yaml
