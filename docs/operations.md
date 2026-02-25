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

Writable snapshot from a read-only snapshot. Instant, independent of source.

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

Agent: `btrfs subvolume snapshot <src>/data <dst>/data` (writable) → stored at `{basePath}/{tenant}/{name}/`

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
- `NodeGetVolumeStats` reads `metadata.json` for quota-aware reporting

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

Default mode: `0750`. Applied at creation via `chown`/`chmod`. Updated on attach if annotations change. Usage updater detects drift from NFS-level changes.

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
