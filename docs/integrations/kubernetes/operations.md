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

**Timeouts:** Export/unexport calls have a 10s context timeout. On failure, Kubernetes retries the operation.

**Mounts:** Uses `k8s.io/mount-utils` for all mount/unmount operations, including stale NFS mount detection (ESTALE, EACCES, EIO) and force unmount fallback (30s timeout).
