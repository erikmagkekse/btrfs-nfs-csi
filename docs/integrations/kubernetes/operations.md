# Kubernetes CSI Operations

See also: [Agent operations](../../operations.md) for CLI and API usage.

## Snapshots

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

Direct clone from an existing PVC. No intermediate snapshot needed.

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

## Expansion

Online. Requires `allowVolumeExpansion: true` in StorageClass.

```yaml
# Just increase storage in the PVC
resources:
  requests:
    storage: 20Gi  # was 10Gi
```

## Compression and NoCOW via Annotations

```yaml
annotations:
  btrfs-nfs-csi/compression: "zstd"
  btrfs-nfs-csi/nocow: "true"
```

Annotations override StorageClass defaults. See [configuration](configuration.md) for all available annotations.

## fsGroup

```yaml
spec:
  securityContext:
    fsGroup: 1000
```

Handled by kubelet via `fsGroupPolicy: File`. Kubelet applies recursive chown + setgid after bind mount.

## UID / GID / Mode via Annotations

```yaml
annotations:
  btrfs-nfs-csi/uid: "1000"
  btrfs-nfs-csi/gid: "1000"
  btrfs-nfs-csi/mode: "0750"
```

Applied at creation. Updated on attach if annotations change. Usage updater detects drift from NFS-level changes.

## NFS Export Lifecycle

ControllerPublish creates an export with K8s metadata labels, NodeStage mounts via NFS, NodePublish bind-mounts into the pod. Reverse on detach.

Exports are [reference-counted per client IP](../../operations.md#nfs-exports).

**Retries:** Controller retries export/unexport 3x with 3s timeout each.

**Mount timeouts:** NFS/bind mount 2min, unmount falls back to `umount -f`.

## NFS Mount Health

The node driver runs a background health checker that detects and auto-heals stale NFS mounts.

**How it works:**

1. Every `DRIVER_HEALTH_CHECK_INTERVAL` (default 30s), the health checker scans active NFS staging mounts
2. Each mount is tested with a 5s stat timeout. A stale mount causes stat to hang or return ESTALE/EIO
3. On stale detection: fresh NFS remount over the stale mount at the same staging path
4. All existing bind mounts (running pods) heal automatically because they share the same VFS path
5. A k8s event is written on the PVC (`MountRemounted`, `MountRemountFailed`, or `MountHealthy`)
6. `VOLUME_CONDITION` is reported via `NodeGetVolumeStats` for kubelet visibility

**No pod restarts required.** The remount at the staging path restores I/O for all pods using that volume.

**Disable:** Set `DRIVER_HEALTH_CHECK_INTERVAL=0`.
