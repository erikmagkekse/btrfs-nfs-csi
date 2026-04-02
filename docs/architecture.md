# Architecture

## Components

| Component | Type | Description |
|---|---|---|
| **Agent** | HTTP server (`:8080`) | Manages btrfs subvolumes, NFS exports, quota. Runs on storage host. |
| **Controller** | gRPC server (CSI) | Translates CSI calls to agent HTTP API. K8s Deployment. |
| **Node Driver** | gRPC server (CSI) | NFS mount + bind mount. Privileged DaemonSet. |
| **CLI** | - | Manage volumes, snapshots, exports, tasks. Set `AGENT_URL` and `AGENT_TOKEN`. |

## Volume Lifecycle

```
CreateVolume   → POST /v1/volumes (btrfs subvolume create, compression, quota, chown)
Publish        → POST /v1/volumes/:name/export (exportfs)
NodeStage      → mount -t nfs server:path staging
NodePublish    → mount --bind staging/data target
NodeUnpublish  → umount target
NodeUnstage    → umount staging
Unpublish      → DELETE /v1/volumes/:name/export
DeleteVolume   → DELETE /v1/volumes/:name (subvolume delete)
```

## ID Formats

| ID | Format | Example |
|---|---|---|
| Volume | `{storageClassName}\|{name}` | `btrfs-nfs\|pvc-abc123` |
| Snapshot | `{storageClassName}\|{name}` | `btrfs-nfs\|snap-abc123` |
| Node | `{nodeName}\|{nodeIP}` | `worker-1\|10.1.0.50` |

## Directory Structure

```
{AGENT_BASE_PATH}/
├── tasks/
│   └── {id}.json          ← persisted task state
└── {tenant}/
    ├── {volume}/
    │   ├── data/           ← btrfs subvolume
    │   └── metadata.json
    ├── snapshots/{name}/
    │   ├── data/           ← read-only btrfs snapshot
    │   └── metadata.json
    └── {clone}/
        ├── data/           ← writable btrfs snapshot
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

## CSI Capabilities

**Controller:** `CREATE_DELETE_VOLUME`, `CREATE_DELETE_SNAPSHOT`, `EXPAND_VOLUME`, `CLONE_VOLUME`, `PUBLISH_UNPUBLISH_VOLUME`, `LIST_VOLUMES`, `LIST_SNAPSHOTS`

**Node:** `STAGE_UNSTAGE_VOLUME`, `GET_VOLUME_STATS`

**Plugin:** `CONTROLLER_SERVICE`

## Sidecars

| Sidecar | Version | Component |
|---|---|---|
| csi-provisioner | v5.3.0 | Controller |
| csi-attacher | v4.11.0 | Controller |
| csi-snapshotter | v8.5.0 | Controller |
| csi-resizer | v2.1.0 | Controller |
| node-driver-registrar | v2.16.0 | Node |
| livenessprobe | v2.18.0 | Both |

All controller sidecars use `--leader-election`.

## RBAC

**Controller** (`btrfs-nfs-csi-controller`): PV/PVC/SC/VolumeAttachment/events/nodes/pods/secrets/leases + snapshot CRDs (full access)

**Node** (`btrfs-nfs-csi-node`): PVC get/patch only

## HA

- **Controller:** 1 replica + leader election. Sidecars elect independently.
- **Node:** DaemonSet, rolling update max 1 unavailable.
- **Agent:** Stateful (local btrfs). NFS reconciler re-exports on restart.

| Failure | Impact | Recovery |
|---|---|---|
| Agent down | No new volumes/exports | Existing NFS mounts continue. Restart agent. |
| Controller down | No provisioning | Leader election recovers. |
| Node driver down | No new mounts on node | DaemonSet restarts. |

## StorageClass Model

Each StorageClass defines one agent + tenant pair:

- `agentURL` parameter → which agent to talk to
- `agentToken` secret → which tenant on that agent (token → tenant mapping via `AGENT_TENANTS`)

Volume IDs use the StorageClass name (`{storageClassName}|{name}`), not the agent URL. The controller resolves the agent URL at runtime from the StorageClass cache. This means agent URLs can change (IP, port) without breaking existing volumes.

## Multi-Tenancy

- One directory per tenant under `AGENT_BASE_PATH`
- Token → tenant mapping via `AGENT_TENANTS`
- All API ops scoped to authenticated tenant
- For stronger isolation: separate agents + separate StorageClasses
