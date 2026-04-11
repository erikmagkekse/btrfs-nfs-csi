# Release v0.10.0

**Previous: v0.9.11** | **Date: 2026-04-06**

This is a major feature release with 50+ commits. The goal of this project is to
provide a universal btrfs+NFS storage backend that works beyond Kubernetes. With
the new standalone CLI, REST API, task system, and label-based multi-tenancy,
the agent is now ready to serve as a foundation for future integrations like
Proxmox, Nomad, or any platform that needs managed btrfs storage with NFS
exports.

Feel free to build your own integration and open a pull request. The agent API
is stable, documented via OpenAPI, and the CLI can be used for general admin
tasks, script automation, backup, and archiving. Future plans include FUSE
access to volumes via the CLI over WebSockets, mTLS support, moving snapshots
between agents, VolumeGroupSnapshots, and automated repeatable tasks. There is
so much we can build on top of this, this release is the new foundation for a multi-purpose storage driver!

> **⚠️ This release contains breaking API changes.** If you are upgrading
> from v0.9.x, please read the Upgrade guide down below, before updating.

---

## New Features

### CLI Tool

- **Full CLI tool**, New `btrfs-nfs-csi` CLI built on urfave/cli with subcommands for managing volumes, snapshots, exports, and tasks from the command line.
- **Watch mode**, Real-time polling with `--watch` flag on get commands for monitoring resource changes.
- **Column filtering**, `--columns` flag to select which table columns are displayed.
- **Label filtering**, Repeatable `--label key=value` flags on all list operations.
- **Delete protection**, Resources created by other identities require `--force` confirmation to delete.
- **Health check command**, Dedicated `health` subcommand for driver health status.
- **Relative resize syntax**, Volume resize supports `+5Gi` relative notation.

### User Labels & Multi-Tenancy

- **User-defined labels**, Custom `key=value` labels on volumes, snapshots, clones, exports, and tasks with validation (max 32 labels, max 8 user-managed).
- **Immutable system labels**, System-managed labels (`created-by`, `clone.source.type`, `clone.source.name`) protected from modification after creation.
- **Default labels**, `DRIVER_DEFAULT_LABELS` env var for controller to stamp all volumes (e.g., cluster identity, environment tags).
- **Export labels**, NFS exports carry `created-by`, Kubernetes metadata (pvc/pv/node), and custom labels.
- **Label-based filtering**, All list API endpoints support filtering by label key=value pairs.

### Task System

- **Formal task system**, New subsystem replacing ad-hoc goroutines with a proper task queue, worker pool, and lifecycle management.
- **Worker pool**, Configurable concurrency (`AGENT_TASK_MAX_CONCURRENT`, default 2).
- **Task lifecycle**, Full status tracking (pending -> running -> completed/failed/cancelled) with progress, error, and result fields.
- **Task timeouts**, Per-task timeout configuration with defaults for scrub (24h) and test (6h).
- **Task cleanup**, Automatic removal of old completed/failed tasks after configurable interval (default 24h).
- **Task API**, REST endpoints for task creation, listing, detail, and cancellation.

### PVC-to-PVC Volume Cloning

- **Direct PVC cloning**, Clone from one PVC to another without intermediate snapshot via `dataSource.kind: PersistentVolumeClaim`.
- **Clone expansion**, Cloned volumes support `allowVolumeExpansion` in StorageClass.
- **Clone resilience**, Clones preserve source volume properties in snapshot metadata; fallback to snapshot properties if source volume is deleted.
- **Snapshot volume properties**, Snapshots store original volume compression, nocow, quota, uid, gid, mode for clone fallback.

### API Pagination & Documentation

- **Cursor-based pagination**, All list endpoints support `limit` and `after` cursor with snapshot isolation for consistent paging across requests.
- **Detail variants**, `?detail=true` query parameter on list endpoints for full metadata responses (labels, paths, timestamps).
- **OpenAPI/Swagger**, Auto-generated spec at `/swagger/swagger.json` with interactive UI (enable with `AGENT_API_SWAGGER_ENABLED`).
- **Landing page**, Root endpoint with version info and optional Swagger link.

### Identity Tracking

- **Creator identity injection**, All mutating operations inject `created-by` label from `AGENT_CSI_IDENTITY` for audit trail.
- **Identity-based delete protection**, Volumes/snapshots can only be deleted by their creator; override requires `--force --yes` or `BTRFS_NFS_CSI_FORCE=true`.

### Export Reference Counting

- **Per-client reference counting**, Kernel NFS export created on first client reference, removed only when last reference is gone.
- **Per-export metadata**, Each export reference stores IP, labels, and creation timestamp (replaces simple client string list).

### Deployment

- **Integration subcommands**, `integration kubernetes controller` and `integration kubernetes driver` replace deprecated top-level commands.
- **Edge release channel**, Separate edge build workflow with `-edge` suffix and optional revision tags.
- **OCI image metadata**, Build embeds `org.opencontainers.image` labels for provenance tracking.
- **Pre-release checks**, CI validates semver, chart version, and vendorHash consistency before release.

---

## Validation Improvements

- **Centralized name validation**, `config.ValidateName()` with consistent regex (1-128 chars: a-z, A-Z, 0-9, `_`, `-`).
- **Compression validation**, Algorithm checked against allowed list (zstd, lzo, zlib) with per-algorithm max levels (zstd:15, zlib:9, lzo:0).
- **UID/GID range validation**, `ValidateUID()` / `ValidateGID()` enforce range 0-65534.
- **File permission validation**, `ValidateMode()` parses octal strings with max 0o7777.
- **Label validation framework**, Key/value regex patterns, max 32 labels total, max 8 user-managed labels.
- **Immutable label enforcement**, `requireImmutableLabels()` and `protectImmutableLabels()` prevent deletion or modification of system labels.
- **Client IP validation**, Rejects wildcards, hostnames, CIDR; only valid IPv4/IPv6 addresses accepted.
- **Request body validation**, API handlers validate request bodies and return `BAD_REQUEST` with specific messages on binding failures.
- **Corrupt metadata detection**, Metadata store distinguishes "not found" from read errors with separate `ErrMetadata` code.
- **Concurrent scrub prevention**, Validates no active scrub task exists before starting a new one (checks both agent and btrfs state).
- **Volume resize guard**, Enforces new size >= current size (allows equal-size no-op, rejects shrink).

---

## Error Handling & Messaging

- **Structured error responses**, All API errors return consistent `{error, code}` JSON with proper HTTP status codes (`BAD_REQUEST`, `NOT_FOUND`, `CONFLICT`, `LOCKED`, `UNAUTHORIZED`, `METADATA`, `INTERNAL`).
- **CLI error translation**, `wrapErr()` converts API errors to user-friendly messages: "volume 'test' not found", "already exists", "is busy".
- **Typed AgentError**, Client-side error type with `IsConflict()`, `IsNotFound()`, `IsLocked()` for programmatic handling without string parsing.
- **Auth failure diagnostics**, Failed auth logged with specific reason (missing header, malformed, invalid encoding, invalid token), client IP, and path.
- **Request error logging**, Middleware logs error requests separately (status >= 400) with tenant, path, client IP, response code, and human-readable duration.
- **Metadata error classification**, New `ErrMetadata` code for corrupt metadata scenarios, separate from `ErrInternal`.
- **Cleanup failure logging**, Subvolume deletion failures during clone logged at warning level for rollback tracking.
- **Validation error type**, `ValidationError` distinguishable from storage/system errors via type assertion.

---

## Model & API Changes

### Breaking Changes

- Volume list responses return `Exports` (count) instead of `Clients` (array of IPs).
- Export endpoints now require request body with labels (was simple client IP string).
- `/v1/exports` returns paginated response with summary/detail variants (was flat list).
- Task creation requires `created-by` label (mandatory system label).
- `ExportVolume` renamed to `CreateVolumeExport`, `UnexportVolume` renamed to `DeleteVolumeExport`.

### New & Changed Models

- **Models package**, Wire-format types moved to independent `agent/api/v1/models/` package, free of external dependencies.
- **Labels on all entities**, `Labels map[string]string` added to volumes, snapshots, clones, exports, and tasks.
- **ExportMetadata**, Replaces simple client strings with structured IP, labels, and creation timestamp per export entry.
- **SnapshotMetadata expansion**, Stores source volume properties (QuotaBytes, NoCOW, Compression, UID, GID, Mode) for clone fallback.
- **Task model**, Formalized `task.Task` type with progress tracking, labels, timeout, result payload (`json.RawMessage`), and lifecycle states.
- **Handler modularization**, Monolithic handler split into `handler_volume.go`, `handler_snapshot.go`, `handler_export.go`, `handler_task.go`, `handler_stats.go` with Swagger annotations.
- **Pagination snapshot cache**, In-memory TTL cache for cursor-based pagination with configurable snapshot count and lifetime.
- **CreatedBy field**, Volume responses include `CreatedBy` extracted from labels.

---

## Security & Hardening

- **Identity injection**, All mutating operations inject `created-by` label from `AGENT_CSI_IDENTITY` for audit trail.
- **Immutable label protection**, System labels locked after creation; configured via `AGENT_IMMUTABLE_LABELS`.
- **Delete protection**, Volumes/snapshots can only be deleted by their creator identity; override requires `--force --yes` or `BTRFS_NFS_CSI_FORCE=true`.
- **Export reference counting**, Kernel NFS export created on first reference, removed on last; prevents orphaned exports.
- **Volume deletion blocked with active exports**, Returns "busy (active exports?)" error when NFS exports exist, preventing accidental data loss.
- **CI workflow hardening**, Explicit `permissions: contents: read` on GitHub Actions workflows; pre-release checks for semver, chart version, and vendorHash consistency.

---

## Dependency Upgrades

- `google.golang.org/grpc` 1.79.3 -> 1.80.0
- `github.com/labstack/echo/v5` 5.0.4 -> 5.1.0
- `github.com/rs/zerolog` 1.34.0 -> 1.35.0
- `azure/setup-helm` 4 -> 5
- Added: `swaggo/swag` v1.16.6, `urfave/cli/v3` v3.8.0, CSI snapshotter client v8.4.0

---

## Upgrade Guide

### A note to existing Kubernetes users

This release adds a lot of new surface area (CLI, REST API, task system, labels)
and lays the groundwork for integrations beyond Kubernetes. If you are using
btrfs-nfs-csi purely as a CSI driver, nothing changes for you. Your Helm values,
StorageClasses, PVCs, and snapshots continue to work exactly as before. The new
features are additive, the Kubernetes integration is fully backwards compatible.

There are a few things to be aware of after upgrading:

- **Volume labels**, Volumes created before v0.10.0 have no labels. Labels are backfilled automatically when pods are rescheduled (`ControllerPublishVolume` sets `created-by`, `kubernetes.pvc.name`, `kubernetes.pvc.namespace`, and `kubernetes.pvc.storageclassname`). No manual action required, labels appear gradually as pods restart (rolling updates, node drains, pod evictions).
- **Snapshot labels**, Snapshots created before v0.10.0 will not have labels. This is purely cosmetic, they continue to work for restores and clones.
- **Stale NFS exports**, The export model changed from a simple client IP list to reference-counted exports with labels. Pre-0.10.0 exports are migrated automatically but may leave orphaned entries. Volumes with stale exports cannot be deleted by the controller (the agent returns "busy"). If this happens, you will see it in the PVC events and controller logs. To clean up:
  1. Scale down or delete the workloads using the affected volumes.
  2. Wait ~3 minutes until the VolumeAttachments are fully removed.
  3. Remove the stale exports: `btrfs-nfs-csi export list`, then `btrfs-nfs-csi export remove <volume> <client>` for each stale entry.
  4. Scale your workloads back up. The controller will create fresh exports with the new reference-counted model.

---

## Deprecations

- `controller` and `driver` top-level commands deprecated in favor of `integration kubernetes controller` and `integration kubernetes driver`.
- Web dashboard feature removed from README and code
