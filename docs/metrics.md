# Metrics

16 metrics across 3 components.

## Agent (6) - port 8080

| Metric | Type | Labels |
|---|---|---|
| `btrfs_nfs_csi_agent_http_requests_total` | Counter | `method`, `path`, `code` |
| `btrfs_nfs_csi_agent_http_request_duration_seconds` | Histogram | `method`, `path` |
| `btrfs_nfs_csi_agent_volumes` | Gauge | `tenant` |
| `btrfs_nfs_csi_agent_exports` | Gauge | `tenant` |
| `btrfs_nfs_csi_agent_volume_size_bytes` | Gauge | `tenant`, `volume` |
| `btrfs_nfs_csi_agent_volume_used_bytes` | Gauge | `tenant`, `volume` |

**Buckets (http_request_duration):** `[0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5]`

## Controller (6) - port 9090

| Metric | Type | Labels |
|---|---|---|
| `btrfs_nfs_csi_controller_grpc_requests_total` | Counter | `method`, `code` |
| `btrfs_nfs_csi_controller_grpc_request_duration_seconds` | Histogram | `method` |
| `btrfs_nfs_csi_controller_agent_ops_total` | Counter | `operation`, `status`, `storage_class` |
| `btrfs_nfs_csi_controller_agent_duration_seconds` | Histogram | `operation`, `storage_class` |
| `btrfs_nfs_csi_controller_agent_health_total` | Counter | `result`, `storage_class` |
| `btrfs_nfs_csi_controller_k8s_ops_total` | Counter | `status` |

**Operations:** `create_volume`, `delete_volume`, `create_snapshot`, `delete_snapshot`, `create_clone`, `export`, `unexport`, `update_volume`

**Status:** `success`, `error`, `conflict`, `not_found`

**Health results:** `healthy`, `error`, `version_mismatch`

**Buckets (agent_duration):** `[0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10]`

## Node (4) - port 9090

| Metric | Type | Labels |
|---|---|---|
| `btrfs_nfs_csi_node_grpc_requests_total` | Counter | `method`, `code` |
| `btrfs_nfs_csi_node_grpc_request_duration_seconds` | Histogram | `method` |
| `btrfs_nfs_csi_node_mount_ops_total` | Counter | `operation`, `status` |
| `btrfs_nfs_csi_node_mount_duration_seconds` | Histogram | `operation` |

**Mount operations:** `nfs_mount`, `bind_mount`, `umount`, `force_umount`, `remount_ro`

**Buckets (grpc_request_duration):** `[0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10]`

**Buckets (mount_duration):** `[0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120]`

## PromQL Examples

```promql
# Volumes near quota (> 90%)
btrfs_nfs_csi_agent_volume_used_bytes / btrfs_nfs_csi_agent_volume_size_bytes > 0.9

# Agent error rate
sum(rate(btrfs_nfs_csi_controller_agent_ops_total{status="error"}[5m]))

# NFS mount failures
rate(btrfs_nfs_csi_node_mount_ops_total{operation="nfs_mount",status="error"}[5m])

# Force unmounts (stuck mounts)
increase(btrfs_nfs_csi_node_mount_ops_total{operation="force_umount"}[1h])

# Agent health failures
rate(btrfs_nfs_csi_controller_agent_health_total{result="error"}[5m])

# P99 mount latency
histogram_quantile(0.99, rate(btrfs_nfs_csi_node_mount_duration_seconds_bucket{operation="nfs_mount"}[5m]))
```
