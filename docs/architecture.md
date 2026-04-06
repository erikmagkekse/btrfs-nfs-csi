# Architecture

## Components

| Component | Description |
|---|---|
| **Agent** | HTTP server (`:8080`). Manages btrfs subvolumes, NFS exports, quotas. Runs on storage host. |
| **CLI** | Manages volumes, snapshots, exports, tasks via agent REST API. Set `AGENT_URL` and `AGENT_TOKEN`. |

Integrations (like the [Kubernetes CSI driver](integrations/kubernetes/)) build on top of the agent API.

## Volume Lifecycle (API)

> Enable `AGENT_API_SWAGGER_ENABLED=true` for the full OpenAPI spec at `/swagger.json`.

```
Create    --> POST /v1/volumes          (btrfs subvolume create, compression, quota, chown)
Export    --> POST /v1/volumes/:name/export   (exportfs, client must be valid IPv4/IPv6)
Unexport  --> DELETE /v1/volumes/:name/export
Delete    --> DELETE /v1/volumes/:name   (subvolume delete)
```

## Directory Structure

```
{AGENT_BASE_PATH}/
├── tasks/
│   └── {id}.json          <-- persisted task state
└── {tenant}/
    ├── {volume}/
    │   ├── data/           <-- btrfs subvolume
    │   └── metadata.json
    ├── snapshots/{name}/
    │   ├── data/           <-- read-only btrfs snapshot
    │   └── metadata.json
    └── {clone}/
        ├── data/           <-- writable btrfs snapshot
        └── metadata.json
```

## Task System

Long-running operations (scrub, future: cross-agent transfers) run as background tasks with progress tracking.

- Worker pool limits concurrent tasks (`AGENT_TASK_MAX_CONCURRENT`, default 2, 0 = unlimited)
- Tasks that exceed the limit queue as `pending` until a slot frees up
- Tasks are persisted as JSON files under `{AGENT_BASE_PATH}/tasks/`
- Progress is tracked in-memory via atomic pointers (no disk IO per update)
- Status transitions (pending, running, completed, failed, cancelled) are persisted to disk
- On agent restart, interrupted running/pending tasks are marked as `failed`
- Completed tasks are cleaned up after `AGENT_TASK_CLEANUP_INTERVAL` (default 24h)

## Pagination

List endpoints use cursor-based pagination with snapshot isolation. On the first page request, the full result set is copied into an in-memory snapshot (keyed by random ID, TTL 30s). Subsequent pages slice from the snapshot, so clients see a consistent view even if the underlying data changes. Expired or invalid cursors fall back to page 1 of current data. Max concurrent snapshots is capped (`AGENT_API_PAGINATION_MAX_SNAPSHOTS`, default 100).

## Cache Loading

On startup, the agent scans the tenant directory and loads volume/snapshot metadata into an in-memory cache. Directories with metadata but no `data/` subdirectory (phantom entries) are skipped with a warning. This prevents stale entries from appearing in API responses after incomplete cleanups.

## Multi-Tenancy

- One directory per tenant under `AGENT_BASE_PATH`
- Token to tenant mapping via `AGENT_TENANTS`
- All API operations scoped to authenticated tenant
- For stronger isolation: separate agents

## HA

> This is an advanced topic. You should be comfortable with DRBD, Pacemaker, and Corosync before attempting this setup.

The agent is stateful (local btrfs). On restart, the NFS reconciler re-exports all active exports automatically. For environments that need failover, the agent supports active/passive HA with DRBD + Pacemaker.

### How it works

1. **DRBD** replicates the btrfs block device between two nodes in real time.
2. **Pacemaker + Corosync** manage the cluster. Corosync handles node communication and quorum, Pacemaker decides which node is active.
3. On failover, Pacemaker promotes the DRBD secondary, mounts the btrfs filesystem, starts the agent container, and moves the floating IP.
4. The agent picks up the existing data directory and re-exports all NFS shares. Existing NFS clients reconnect transparently (NFS handles server failover via the floating IP).

### What you need

- 2 nodes with identical btrfs block devices
- DRBD 9 configured for synchronous replication
- Pacemaker + Corosync cluster with fencing (STONITH)
- A floating IP (or VIP) that moves with the active node

| Failure | Impact | Recovery |
|---|---|---|
| Agent down | No new volumes/exports | Existing NFS mounts continue. Restart agent. |
| Active node down | Brief interruption | Pacemaker promotes standby node, NFS clients reconnect. |
