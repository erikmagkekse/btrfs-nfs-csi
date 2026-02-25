# Changelog

## v0.9.6

Some hotfixes and a requested configuration option.

**Note:** Default data mode changed from `0750` to `2770` (setgid + group rwx). Only affects new volumes. Set `AGENT_DEFAULT_DATA_MODE=0750` to keep the old behavior.

- Configurable default directory and data mode via `AGENT_DEFAULT_DIR_MODE` and `AGENT_DEFAULT_DATA_MODE`
- Validate mode values at startup
- Fix LastAttachAt showing `0001-01-01` for unattached volumes
- Fix usage updater skipping volumes on qgroup query failure

## v0.9.5 - Initial Beta Release

### Features

- btrfs subvolume management (create, delete, snapshot, clone)
- Online volume expansion via qgroup limits
- Per-volume compression (zstd, lzo, zlib)
- NoCOW mode for databases
- Per-volume UID/GID/mode
- Automatic per-node NFS exports via exportfs
- Multi-tenant support
- NFS export reconciler
- Prometheus metrics on all components
- Web dashboard
- TLS support
- HA via DRBD + Pacemaker
