# Configuration

## Agent Environment Variables

| Variable | Default | Description |
|---|---|---|
| `AGENT_BASE_PATH` | `./storage` | btrfs mount point |
| `AGENT_TENANTS` | **required** | `name:token,name:token` |
| `AGENT_LISTEN_ADDR` | `:8080` | HTTP listen address |
| `AGENT_METRICS_ADDR` | `127.0.0.1:9090` | Metrics server address |
| `AGENT_TLS_CERT` | - | TLS certificate path |
| `AGENT_TLS_KEY` | - | TLS key path |
| `AGENT_FEATURE_QUOTA_ENABLED` | `true` | btrfs quota tracking |
| `AGENT_FEATURE_QUOTA_UPDATE_INTERVAL` | `1m` | Usage update interval |
| `AGENT_NFS_EXPORTER` | `kernel` | NFS exporter type |
| `AGENT_EXPORTFS_BIN` | `exportfs` | exportfs binary path |
| `AGENT_KERNEL_EXPORT_OPTIONS` | `rw,nohide,crossmnt,no_root_squash,no_subtree_check` | NFS export options (fsid is always appended automatically) |
| `AGENT_BTRFS_BIN` | `btrfs` | btrfs binary path |
| `AGENT_NFS_RECONCILE_INTERVAL` | `10m` | Export reconciliation (`0` = off) |
| `AGENT_DEVICE_IO_INTERVAL` | `5s` | Device IO stats update interval |
| `AGENT_DEVICE_STATS_INTERVAL` | `1m` | btrfs device errors + filesystem usage update interval |
| `AGENT_DASHBOARD_REFRESH_SECONDS` | `5` | Dashboard refresh |
| `AGENT_DEFAULT_DIR_MODE` | `0700` | Default mode for volume/snapshot/clone directories |
| `AGENT_DEFAULT_DATA_MODE` | `2770` | Default mode for data subvolumes (setgid + group rwx) |
| `AGENT_TASK_CLEANUP_INTERVAL` | `24h` | Remove completed/failed tasks after this duration |
| `AGENT_TASK_MAX_CONCURRENT` | `2` | Max concurrent tasks (`0` = unlimited) |
| `AGENT_TASK_DEFAULT_TIMEOUT` | `6h` | Default timeout for tasks (e.g. test). `0` = no timeout |
| `AGENT_TASK_SCRUB_TIMEOUT` | `24h` | Timeout for btrfs scrub tasks. `0` = no timeout |
| `AGENT_TASK_POLL_INTERVAL` | `5s` | Progress update interval for background tasks |
| `LOG_LEVEL` | `info` | `trace`, `debug`, `info`, `warn`, `error` |

## CLI Environment Variables

| Variable | Default | Description |
|---|---|---|
| `AGENT_URL` | - | Agent API URL |
| `AGENT_TOKEN` | - | Tenant token |

Also configurable via `--agent-url` and `--agent-token` flags.

## Controller Environment Variables

| Variable | Default | Description |
|---|---|---|
| `DRIVER_ENDPOINT` | `unix:///csi/csi.sock` | gRPC socket |
| `DRIVER_METRICS_ADDR` | `:9090` | Metrics address |
| `DRIVER_DEFAULT_LABELS` | - | Default labels for all volumes (`key=value,key=value`) |

## Node Environment Variables

| Variable | Default | Description |
|---|---|---|
| `DRIVER_NODE_ID` | **required** | Node name (`spec.nodeName`) |
| `DRIVER_NODE_IP` | - | Static IP (fallback) |
| `DRIVER_STORAGE_INTERFACE` | - | Storage NIC name (priority 1) |
| `DRIVER_STORAGE_CIDR` | - | Storage subnet CIDR (priority 2) |
| `DRIVER_ENDPOINT` | `unix:///csi/csi.sock` | gRPC socket |
| `DRIVER_METRICS_ADDR` | `:9090` | Metrics address |
| `DRIVER_HEALTH_CHECK_INTERVAL` | `30s` | NFS mount health check interval. Set to `0` to disable. |

**IP resolution order:** `DRIVER_STORAGE_INTERFACE` > `DRIVER_STORAGE_CIDR` > `DRIVER_NODE_IP`. At least one required.

**Note:** `DRIVER_STORAGE_INTERFACE` and `DRIVER_STORAGE_CIDR` resolve IPs from the host's network interfaces. The node DaemonSet must have `hostNetwork: true` for this to work.

## StorageClass Parameters

Each StorageClass binds one agent + one tenant. The SC name is used in volume IDs (`{storageClassName}|{volumeName}`) - do not rename it after creating volumes. The `agentURL` can be changed safely (e.g. IP change, port change).

| Parameter | Required | Description |
|---|---|---|
| `nfsServer` | yes | NFS server IP |
| `agentURL` | yes | Agent REST API URL |
| `nfsMountOptions` | no | NFS mount options |
| `nocow` | no | `"true"` / `"false"` |
| `compression` | no | `zstd`, `lzo`, `zlib`, `none` (with level: `zstd:3`) |
| `uid` / `gid` | no | Volume owner |
| `mode` | no | Octal permissions (default `"2770"`) |

## PVC Annotations

| Annotation | Values |
|---|---|
| `btrfs-nfs-csi/nocow` | `"true"`, `"false"` |
| `btrfs-nfs-csi/compression` | `"zstd"`, `"lzo"`, `"zlib"`, `"none"` |
| `btrfs-nfs-csi/uid` | integer |
| `btrfs-nfs-csi/gid` | integer |
| `btrfs-nfs-csi/mode` | octal string |
| `btrfs-nfs-csi/labels` | `key=value,key=value` (max 4 user labels, see below) |

Annotations override StorageClass defaults. Applied at create and on every attach.

### Default Labels

The CSI controller automatically sets these labels on every PVC volume:

| Label | Value | Reserved |
|---|---|---|
| `kubernetes.pvc.name` | PVC name | yes |
| `kubernetes.pvc.namespace` | PVC namespace | yes |
| `kubernetes.pvc.storageclass` | StorageClass name | yes |
| `created-by` | `csi` | no (overridable) |

Reserved keys cannot be overridden via PVC annotation (set by user will be skipped with a warning). This restriction only applies to the CSI controller, the agent API and CLI have no reserved keys. The `created-by` label can be overridden. Max 4 user labels via annotation (max 12 total).

### Custom Default Labels

Set `DRIVER_DEFAULT_LABELS` on the controller to add custom defaults to every volume:

```yaml
env:
  - name: DRIVER_DEFAULT_LABELS
    value: "kubernetes.cluster=my-cluster,env=prod"
```

These are merged after the built-in defaults but before user annotation labels. Reserved keys (`kubernetes.pvc.*`) are ignored. User annotation labels override env defaults on key conflict.

## Secret

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: btrfs-nfs-creds
  namespace: btrfs-nfs-csi
type: Opaque
stringData:
  agentToken: "changeme"  # must match AGENT_TENANTS token
```

## TLS

Set `AGENT_TLS_CERT` + `AGENT_TLS_KEY` → agent listens HTTPS (min TLS 1.2). Use `https://` in `agentURL`.
