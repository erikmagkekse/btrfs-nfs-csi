# Operations

## Snapshots

Read-only btrfs snapshots. Instant, CoW-efficient.

```yaml
apiVersion: snapshot.storage.k8s.io/v1
kind: VolumeSnapshot
metadata:
  name: my-snap
spec:
  volumeSnapshotClassName: btrfs-nfs
  source:
    persistentVolumeClaimName: my-pvc
```

Agent: `btrfs subvolume snapshot -r <src>/data <dst>/data` → stored under `{basePath}/{tenant}/snapshots/{name}/`

Usage updater tracks `used_bytes` (referenced) and `exclusive_bytes` (unique blocks).

## Clones

### From Snapshot

Writable clone from a read-only VolumeSnapshot. Instant, independent of source.

```yaml
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: my-clone
spec:
  storageClassName: btrfs-nfs
  accessModes: [ReadWriteOnce]
  resources:
    requests:
      storage: 10Gi
  dataSource:
    name: my-snap
    kind: VolumeSnapshot
    apiGroup: snapshot.storage.k8s.io
```

### From PVC (PVC-to-PVC)

Direct clone from an existing PVC. No intermediate snapshot needed, a single atomic `btrfs subvolume snapshot` under the hood.

```yaml
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: my-clone
spec:
  storageClassName: btrfs-nfs
  accessModes: [ReadWriteOnce]
  resources:
    requests:
      storage: 10Gi
  dataSource:
    name: source-pvc
    kind: PersistentVolumeClaim
```

Both clone types are instant (btrfs CoW), independent of the source, and stored at `{basePath}/{tenant}/{name}/`.

## Expansion

Online - updates btrfs qgroup limit only. No node expansion needed.

```yaml
# Just increase storage in the PVC
resources:
  requests:
    storage: 20Gi  # was 10Gi
```

Requires `allowVolumeExpansion: true` in StorageClass. New size must be > current size.

## Compression

| Algorithm | Notes |
|---|---|
| `zstd` | Recommended. Optional level: `zstd:3` (1–15) |
| `lzo` | Fastest, lower ratio |
| `zlib` | Highest ratio, slowest |
| `none` | Disable |

Set via SC parameter `compression` or PVC annotation `btrfs-nfs-csi/compression`.

Applies to new writes only. Mutually exclusive with NoCOW.

## NoCOW

`chattr +C` - disables copy-on-write. Use for databases, VM images.

```yaml
annotations:
  btrfs-nfs-csi/nocow: "true"
```

Trade-off: no snapshots/clones, no checksums, no compression. Better random write performance.

## Quota

Enabled by default (`AGENT_FEATURE_QUOTA_ENABLED=true`).

- Create: `btrfs qgroup limit <bytes> <path>`
- Usage updater: polls `btrfs qgroup show` at `AGENT_FEATURE_QUOTA_UPDATE_INTERVAL`
- `NodeGetVolumeStats` reads `metadata.json` for quota-aware reporting. Falls back to `statfs` when quota is disabled.

## fsGroup

```yaml
spec:
  securityContext:
    fsGroup: 1000
```

Handled by kubelet via `fsGroupPolicy: File` (set in setup.yaml). Kubelet applies recursive chown + setgid after bind mount.

## UID / GID / Mode

```yaml
annotations:
  btrfs-nfs-csi/uid: "1000"
  btrfs-nfs-csi/gid: "1000"
  btrfs-nfs-csi/mode: "0750"
```

Default mode: `2770` (configurable via `AGENT_DEFAULT_DATA_MODE`). Applied at creation via `chown`/`chmod`. Updated on attach if annotations change. Usage updater detects drift from NFS-level changes.

## NFS Exports

Export options: `rw,nohide,crossmnt,no_root_squash,no_subtree_check,fsid=<crc32>`

**Lifecycle:** ControllerPublish → `exportfs` add → NodeStage (NFS mount) → NodePublish (bind mount) → reverse on detach.

**Reconciler** (every `AGENT_NFS_RECONCILE_INTERVAL`):
- Removes orphaned exports (path deleted)
- Re-adds missing exports from metadata (agent restart recovery)

**Retries:** Controller retries export/unexport 3x with 3s timeout each.

**Recommended mount options:**
```
nfsvers=4.2,hard,noatime,rsize=1048576,wsize=1048576,nconnect=8
```

**Mount timeouts:** NFS/bind mount 2min, unmount falls back to `umount -f`.

## NFS Mount Health

The node driver runs a background health checker that detects and auto-heals stale NFS mounts. This handles the case where an NFS server goes down while pods are running, causing mounts to hang indefinitely on hard-mount NFS.

**How it works:**

1. Every `DRIVER_HEALTH_CHECK_INTERVAL` (default 30s), the health checker scans active mounts for NFS staging mounts
2. Each mount is tested with a 5s stat timeout. A stale mount causes stat to hang or return ESTALE/EIO
3. On stale detection: fresh NFS remount over the stale mount at the same staging path
4. All existing bind mounts (running pods) heal automatically because they share the same VFS path
5. A k8s Warning event is written on the PVC (`MountAutoHealed` or `MountRemountFailed`)
6. `VOLUME_CONDITION` is reported via `NodeGetVolumeStats` for kubelet visibility

**No pod restarts required.** The remount at the staging path restores I/O for all pods using that volume.

**Monitoring:**

```promql
# Stale mounts detected per hour
increase(btrfs_nfs_csi_node_health_checks_total{result="stale"}[1h])

# Failed remounts (needs operator attention)
increase(btrfs_nfs_csi_node_health_checks_total{result="remount_failed"}[1h]) > 0
```

**Disable:** Set `DRIVER_HEALTH_CHECK_INTERVAL=0`.

## Scrub

btrfs scrub verifies data integrity by reading all blocks and checking checksums. Runs as a background task via the task system.

```bash
# Start scrub
curl -X POST http://10.0.0.5:8080/v1/tasks/scrub \
  -H "Authorization: Bearer changeme"
# {"task_id": "abc123", "status": "pending"}

# Poll progress
curl http://10.0.0.5:8080/v1/tasks/abc123 \
  -H "Authorization: Bearer changeme"
# {"status": "running", "progress": 42, ...}

# Cancel
curl -X DELETE http://10.0.0.5:8080/v1/tasks/abc123 \
  -H "Authorization: Bearer changeme"
```

Only one scrub can run at a time per filesystem. The agent detects externally started scrubs (e.g. via `btrfs scrub start` on the host) and rejects duplicates.

Completed tasks include a result with bytes scrubbed and error counts. Tasks are persisted to disk and cleaned up after `AGENT_TASK_CLEANUP_INTERVAL` (default 24h).

## CLI

The `btrfs-nfs-csi` binary doubles as a CLI tool. Any command that isn't `agent`, `controller`, or `driver` is treated as a CLI command.

```bash
export AGENT_URL=http://10.0.0.5:8080
export AGENT_TOKEN=changeme

btrfs-nfs-csi volume list
btrfs-nfs-csi volume list -o wide
btrfs-nfs-csi volume list -o json
btrfs-nfs-csi volume get my-vol
btrfs-nfs-csi volume create my-vol 10Gi --compression zstd
btrfs-nfs-csi volume expand my-vol 20Gi
btrfs-nfs-csi volume clone source-vol new-vol
btrfs-nfs-csi volume delete my-vol --confirm --yes

btrfs-nfs-csi snapshot list
btrfs-nfs-csi snapshot list my-vol          # filter by volume
btrfs-nfs-csi snapshot create my-vol snap-1
btrfs-nfs-csi snapshot clone snap-1 new-vol # clone from snapshot
btrfs-nfs-csi snapshot delete snap-1 --confirm --yes

btrfs-nfs-csi export list
btrfs-nfs-csi export add my-vol 10.1.0.50
btrfs-nfs-csi export remove my-vol 10.1.0.50

btrfs-nfs-csi task list
btrfs-nfs-csi task list --type scrub
btrfs-nfs-csi task get <id>
btrfs-nfs-csi task cancel <id>
btrfs-nfs-csi task create scrub
btrfs-nfs-csi task create scrub --wait
btrfs-nfs-csi task create test
btrfs-nfs-csi task create test --sleep 10s --wait
btrfs-nfs-csi stats
btrfs-nfs-csi stats -o wide                 # per-device IO and error details
btrfs-nfs-csi health
btrfs-nfs-csi version
```

**Global flags:** `--agent-url`, `--agent-token`, `--output` / `-o` (table, wide, json).

**Output formats:** `table` (default), `wide` (extra columns), `json` (raw API response). Combine with `-o json,wide` for detailed JSON.

**Sorting:** `--sort` / `-s` with `--asc` (default descending). Volume default: `used%`. Snapshot default: `created`.

**Size values:** Supports `Ki`, `Mi`, `Gi` (binary) and `K`, `M`, `G` (decimal).
