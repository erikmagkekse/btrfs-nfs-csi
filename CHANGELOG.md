# Changelog

## v0.9.11

This release focuses on reliability and broader hardware support: multi-device btrfs, stale NFS mount recovery via `k8s.io/mount-utils`, and safe volume deletion when NFS exports are active.

### Features
- BTRFS multi-device support and improved container device resolution (#76)
- Support 128-character volume and snapshot names (#78)
- Block volume deletion when NFS exports are still active (#87)

### Improvements
- Use `/proc/self/mountinfo` for mount point resolution (#86)
- Use `k8s.io/mount-utils` for stale NFS mount handling and mount operations in driver (#84)
- Missing device handling, degraded health reporting, and stats API restructure (#82)
- Remove retry logic from controller publish/unpublish (#75)

### Bug Fixes
- Fix btrfs startup check for subdirectory base paths (#77)
- Fix device symlink resolution for LVM/device-mapper sysfs stats (#74)

### Dependencies
- Bump `github.com/rs/zerolog` from 1.34.0 to 1.35.0 (#79)
- Bump `azure/setup-helm` from 4 to 5 (#80)

## v0.9.10

This release adds the Helm chart as the primary deployment method for the CSI driver and controller. It also fixes agent tracking when multiple StorageClasses share the same agent and consolidates health check metrics into `agent_ops_total`.

### Features
- Helm chart for CSI driver and controller deployment (#67)
- Configurable kernel NFS export options via `AGENT_KERNEL_EXPORT_OPTIONS` (#66)
- Helm Release workflow with `azure/setup-helm` + `docker/login-action` (#71)
- appVersion/VERSION mismatch check in Helm CI and release workflows (#71)

### Improvements
- Validate NoCOW and Compression values in controller (#68)

### Bug Fixes
- Fix multi-SC agent tracking: resolve StorageClass name from PVC instead of broken reverse-lookup (#68)
- Fix `snapshotClass: false` and `allowVolumeExpansion: false` ignored in Helm chart (#71)
- Fix device sysfs stat lookup for LVM, mdraid, bcache and other device-mapper setups where the block device is a symlink (#73)

### Refactoring
- Move `IsValidCompression` to shared `utils/` package (#68)
- Rename `helm.yml` to `ci-helm.yml`, add `helm-release.yml` as reusable workflow (#71)

### Breaking Changes
- `btrfs_nfs_csi_controller_agent_health_total` metric removed — use `agent_ops_total{operation="health_check"}` instead (#69)
- Health checks now tracked in `agent_ops_total` and `agent_duration_seconds` with `operation=health_check` (#69)

## v0.9.9

### Features
- Sortable columns (Usage %, Clients, Created) on dashboard volume and snapshot tables (#60)
- ReadOnlyMany (ROX) access mode support for read-only PVC mounts (#57)
- Nix flake for package and NixOS service module, thanks to @nikp123 (#53)
- Adding the ability to have multiple agents in the Nix module, thanks to @nikp123 (#62)
- Improve dashboard formatting and UX (#64)
- Simplify snapshot table view in dashboard (#65)

### Bug Fixes
- Fix read-only PVC mounts not being remounted as read-only (#57)
- Fix NodeGetVolumeStats always failing with "agent may be down" (#63)

### Refactoring
- Unit tests for controller utils (paginate, parseVolumeID, parseNodeIP) and PVC validation (#59)
- Unit tests for driver utils (ResolveNodeIP) (#59)

### Security
- Bump `google.golang.org/grpc` to v1.79.3 — fixes CVE-2026-33186 (authorization bypass via missing leading slash in `:path`, CVSS 9.1 Critical)

### Other
- Improved controller agent version check message on non-matching commit builds, issue #54 (#56)
- Updated Go dependencies and CI workflow (#58)


## v0.9.8

### Features
- `/v1/stats` API endpoint with device IO counters, btrfs device errors, and filesystem allocation (#40)
- 16 new Prometheus metrics for device IO, errors, and filesystem usage (#40)
- IO throughput and device stats graphs on the web dashboard (#40)
- Dedicated plain HTTP metrics server on `127.0.0.1:9090` via `AGENT_METRICS_ADDR` (#41)

### Testing
- NFS integration tests with real kernel exporter (#37, #38, #39)
- Unit tests for reconciler and API error mapping (#37, #38, #39)
- Unit and integration tests for agent storage layer (volumes, snapshots, clones, exports, metadata, usage, utils) (#37, #38, #39)
- Migrated all tests to testify (#37, #38, #39)
- Race detection and `gofmt` check added to CI (#37, #38, #39)

### Refactoring
- Storage layer split into separate files: volume, snapshot, clone, export, metadata, stats
- Moved `/metrics` from authenticated API server (`:8080`) to dedicated metrics server (#41)

### Other
- Updated README: pre-1.0 notice, removed early-stage warning
- Added block size parameter to mixed-load script


## v0.9.7

### Features
- CSI ListVolumes and ListSnapshots RPCs (#24)
- Configurable btrfs binary path via `AGENT_BTRFS_BIN` (#30)
- Improved dashboard snapshot table and detail panel (#31)

### Improvements
- Improved agent API and dashboard UX (#23)
- Synced docs, scripts, and CI (#33)

### Refactoring
- NFS kernel exporter refactored with unit tests (#19)
- Btrfs refactored to Manager pattern with unit + integration tests (#20)
- Driver/controller split, separate CSI identity server, consolidated constants (#29)
- Agent refactor: renamed model to config, improved panic handling and warnings (#32)

### Bug Fixes
- Fix nil pointer panic on volume/clone conflict (#26)
- Fix handler response type mismatches for Create/Update endpoints (#27)


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
