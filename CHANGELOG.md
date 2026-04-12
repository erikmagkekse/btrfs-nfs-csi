# Changelog

## v0.10.0

Major feature release. The agent is now a multi-purpose storage backend with a standalone CLI, REST API, task system, and label-based multi-tenancy. 

### Features
- CLI tool with subcommands for volumes, snapshots, exports, and tasks (#94)
- CLI watch mode, column filtering, label filtering, delete protection (#104, #112, #113)
- CLI volume label commands, show path in get output (#128)
- User-defined labels on volumes, snapshots, clones, exports, and tasks (#100)
- Snapshot labels, clone tracking, controller identity (#108)
- Export reference counting with labels and immutable label protection (#107)
- Task system with btrfs scrub, worker pool, timeouts, and cleanup (#93, #96, #99)
- PVC-to-PVC volume cloning with clone expansion (#91)
- Cursor-based pagination for all list endpoints (#103)
- Client identity injection, TLS/timeout env config (#109)
- OpenAPI/Swagger documentation (#114)
- Squota (simple quotas) support detection at startup (#127)
- Backfill volume labels on ControllerPublishVolume (#129)
- Allow setting created-by on migrated volumes (#117)
- Snapshot volume properties for clone resilience (#115)
- Edge release channel with OCI metadata (#106)

### Improvements
- Bulk qgroup query in usage updater to eliminate N+1 btrfs commands (#126)
- Drop NFS auto-heal, handle stale mounts inline in stage/publish (#124)
- Agent API restructure, storage hardening, clone metadata, validation (#114)
- Improve error handling and logging across all components (#95)
- Improve logging: split volume IDs, gRPC trace level (#102)
- Path-aware metadata cache (#105)
- Migrate custom k8s client to client-go (#101)
- Move snapshot clone under snapshot subcommand (#98)
- Restructure docs, new README, re-record GIFs (#118)
- Update release workflow to trigger on version tags (#120)

### Bug Fixes
- Add missing flock package to container (#134)
- Serialize concurrent same-name creates to prevent 500s and metadata loss (#133)
- Harden driver defaults (#116)
- Fixed CI workflows (#110)

### Breaking Changes
- Volume list responses return `Exports` (count) instead of `Clients` (array of IPs)
- Export model changed from simple client IP list to reference-counted exports with labels (pre-0.10.0 exports are auto-migrated)
- Export endpoints now require request body with labels
- `/v1/exports` returns paginated response with summary/detail variants
- `ExportVolume` renamed to `CreateVolumeExport`, `UnexportVolume` renamed to `DeleteVolumeExport`
- `controller` and `driver` top-level commands deprecated in favor of `integration kubernetes controller` and `integration kubernetes driver`
- Web dashboard removed

### Dependencies
- Bump `google.golang.org/grpc` from 1.79.3 to 1.80.0 (#92)
- Bump `github.com/labstack/echo/v5` from 5.0.4 to 5.1.0 (#90)
- Bump `golang.org/x/sys` from 0.42.0 to 0.43.0 (#123)
- Bump `golang.org/x/term` from 0.41.0 to 0.42.0 (#125)
- Bump `actions/download-artifact` from 4 to 8 (#121)
- Bump `actions/upload-artifact` from 4 to 7 (#122)
- Added: `swaggo/swag` v1.16.6, `urfave/cli/v3` v3.8.0, CSI snapshotter client v8.4.0

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
