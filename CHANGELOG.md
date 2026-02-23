# Changelog

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
