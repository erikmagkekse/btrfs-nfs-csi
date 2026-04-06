# Kubernetes CSI Architecture

## Components

| Component | Runs on | Role |
|---|---|---|
| **CSI Controller** | Kubernetes Deployment (1 replica) | Translates CSI calls to agent API |
| **CSI Node Driver** | DaemonSet on every node | NFS mounts, bind mounts, health checks |

Each StorageClass binds one agent and one tenant. Volume IDs use the StorageClass name, so agent URLs can change without breaking existing volumes.

## Volume Lifecycle

```
CreateVolume   --> POST /v1/volumes
Publish        --> POST /v1/volumes/:name/export
NodeStage      --> mount -t nfs server:path staging
NodePublish    --> mount --bind staging/data target
NodeUnpublish  --> umount target
NodeUnstage    --> umount staging
Unpublish      --> DELETE /v1/volumes/:name/export
DeleteVolume   --> DELETE /v1/volumes/:name
```

## ID Formats

| ID | Format | Example |
|---|---|---|
| Volume | `{storageClassName}\|{name}` | `btrfs-nfs\|pvc-abc123` |
| Snapshot | `{storageClassName}\|{name}` | `btrfs-nfs\|snap-abc123` |
| Node | `{nodeName}\|{nodeIP}` | `worker-1\|10.1.0.50` |

## StorageClass Model

Each StorageClass defines one agent + tenant pair:

- `agentURL` parameter: which agent to talk to
- `agentToken` secret: which tenant on that agent (token to tenant mapping via `AGENT_TENANTS`)

Volume IDs use the StorageClass name (`{storageClassName}|{name}`), not the agent URL. The controller resolves the agent URL at runtime from the StorageClass cache. This means agent URLs can change (IP, port) without breaking existing volumes.

## CSI Capabilities

**Controller:** `CREATE_DELETE_VOLUME`, `CREATE_DELETE_SNAPSHOT`, `EXPAND_VOLUME`, `CLONE_VOLUME`, `PUBLISH_UNPUBLISH_VOLUME`, `LIST_VOLUMES`, `LIST_SNAPSHOTS`

**Node:** `STAGE_UNSTAGE_VOLUME`, `GET_VOLUME_STATS`, `VOLUME_CONDITION`

| Capability | Supported |
|---|---|
| Dynamic provisioning | Yes |
| Volume expansion | Yes (online, relative `+5Gi`) |
| Snapshots | Yes (instant, btrfs CoW) |
| Clones (snapshot -> volume) | Yes (zero-copy) |
| Clones (volume -> volume) | Yes (zero-copy) |
| ReadWriteMany | Yes (NFS) |
| Volume stats | Yes |
| Volume health | Yes |
| NFS stale mount detection | Yes (auto-heal) |

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

**Node** (`btrfs-nfs-csi-node`): PVC get/patch, VolumeAttachments get/list, PV get, events create

## HA

- **Controller:** 1 replica + leader election. Sidecars elect independently.
- **Node:** DaemonSet, rolling update max 1 unavailable.
- **Node health checker:** Background goroutine detects stale NFS mounts via `statWithTimeout` and auto-heals by remounting. Uses per-volume locking to avoid racing with unstage/unpublish. Heals all bind mounts automatically. Reports `VOLUME_CONDITION` and writes k8s events on PVCs. Configurable via `DRIVER_HEALTH_CHECK_INTERVAL` (default 30s).

| Failure | Impact | Recovery |
|---|---|---|
| Controller down | No provisioning | Leader election recovers. |
| Node driver down | No new mounts on node | DaemonSet restarts. |
