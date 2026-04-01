# Metrics

38 metrics across 3 components.

## Agent (29) - port 9090

| Metric | Type | Labels |
|---|---|---|
| `btrfs_nfs_csi_agent_http_requests_total` | Counter | `method`, `path`, `code` |
| `btrfs_nfs_csi_agent_http_request_duration_seconds` | Histogram | `method`, `path` |
| `btrfs_nfs_csi_agent_volumes` | Gauge | `tenant` |
| `btrfs_nfs_csi_agent_exports` | Gauge | `tenant` |
| `btrfs_nfs_csi_agent_volume_size_bytes` | Gauge | `tenant`, `volume` |
| `btrfs_nfs_csi_agent_volume_used_bytes` | Gauge | `tenant`, `volume` |
| `btrfs_nfs_csi_agent_device_read_bytes_total` | Gauge | `device` |
| `btrfs_nfs_csi_agent_device_read_ios_total` | Gauge | `device` |
| `btrfs_nfs_csi_agent_device_read_time_seconds_total` | Gauge | `device` |
| `btrfs_nfs_csi_agent_device_write_bytes_total` | Gauge | `device` |
| `btrfs_nfs_csi_agent_device_write_ios_total` | Gauge | `device` |
| `btrfs_nfs_csi_agent_device_write_time_seconds_total` | Gauge | `device` |
| `btrfs_nfs_csi_agent_device_ios_in_progress` | Gauge | `device` |
| `btrfs_nfs_csi_agent_device_io_time_seconds_total` | Gauge | `device` |
| `btrfs_nfs_csi_agent_device_io_weighted_time_seconds_total` | Gauge | `device` |
| `btrfs_nfs_csi_agent_device_size_bytes` | Gauge | `device` |
| `btrfs_nfs_csi_agent_device_allocated_bytes` | Gauge | `device` |
| `btrfs_nfs_csi_agent_device_present` | Gauge | `device` |
| `btrfs_nfs_csi_agent_device_read_errs_total` | Gauge | `device` |
| `btrfs_nfs_csi_agent_device_write_errs_total` | Gauge | `device` |
| `btrfs_nfs_csi_agent_device_flush_errs_total` | Gauge | `device` |
| `btrfs_nfs_csi_agent_device_corruption_errs_total` | Gauge | `device` |
| `btrfs_nfs_csi_agent_device_generation_errs_total` | Gauge | `device` |
| `btrfs_nfs_csi_agent_filesystem_size_bytes` | Gauge | `path` |
| `btrfs_nfs_csi_agent_filesystem_used_bytes` | Gauge | `path` |
| `btrfs_nfs_csi_agent_filesystem_unallocated_bytes` | Gauge | `path` |
| `btrfs_nfs_csi_agent_filesystem_metadata_used_bytes` | Gauge | `path` |
| `btrfs_nfs_csi_agent_filesystem_metadata_total_bytes` | Gauge | `path` |
| `btrfs_nfs_csi_agent_filesystem_data_ratio` | Gauge | `path` |

**Buckets (http_request_duration):** `[0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5]`

Device IO metrics are updated every 5s (configurable via `AGENT_DEVICE_IO_INTERVAL`). Device errors and filesystem allocation are updated every 1m (configurable via `AGENT_DEVICE_STATS_INTERVAL`). Missing devices (e.g. physically removed drives in a RAID setup) are skipped during IO polling.

## Controller (5) - port 9090

| Metric | Type | Labels |
|---|---|---|
| `btrfs_nfs_csi_controller_grpc_requests_total` | Counter | `method`, `code` |
| `btrfs_nfs_csi_controller_grpc_request_duration_seconds` | Histogram | `method` |
| `btrfs_nfs_csi_controller_agent_ops_total` | Counter | `operation`, `status`, `storage_class` |
| `btrfs_nfs_csi_controller_agent_duration_seconds` | Histogram | `operation`, `storage_class` |
| `btrfs_nfs_csi_controller_k8s_ops_total` | Counter | `status` |

**Operations and their status values:**

| Operation | Status |
|---|---|
| `create_volume` | `success`, `error`, `conflict` |
| `delete_volume` | `success`, `error`, `not_found` |
| `create_snapshot` | `success`, `error`, `conflict` |
| `delete_snapshot` | `success`, `error`, `not_found` |
| `create_clone` | `success`, `error`, `conflict` |
| `clone_volume` | `success`, `error`, `conflict` |
| `export` | `success`, `error` |
| `unexport` | `success`, `error`, `not_found` |
| `update_volume` | `success`, `error` |
| `list_volumes` | `success`, `error` |
| `list_snapshots` | `success`, `error` |
| `health_check` | `healthy`, `degraded`, `error`, `version_mismatch` |


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

# Missing device alert
btrfs_nfs_csi_agent_device_present == 0

# Device allocation > 90%
btrfs_nfs_csi_agent_device_allocated_bytes / btrfs_nfs_csi_agent_device_size_bytes > 0.9

# Agent error rate
sum(rate(btrfs_nfs_csi_controller_agent_ops_total{status="error"}[5m]))

# NFS mount failures
rate(btrfs_nfs_csi_node_mount_ops_total{operation="nfs_mount",status="error"}[5m])

# Force unmounts (stuck mounts)
increase(btrfs_nfs_csi_node_mount_ops_total{operation="force_umount"}[1h])

# Agent health check errors
rate(btrfs_nfs_csi_controller_agent_ops_total{operation="health_check",status="error"}[5m])

# P99 mount latency
histogram_quantile(0.99, rate(btrfs_nfs_csi_node_mount_duration_seconds_bucket{operation="nfs_mount"}[5m]))
```
