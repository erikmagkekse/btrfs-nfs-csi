# Kubernetes Integration (CSI Driver)

This is where it all started. btrfs-nfs-csi was born as a Kubernetes CSI driver. The built-in CSI integration exposes the agent as a native Kubernetes storage backend. PVCs become btrfs subvolumes, VolumeSnapshots become btrfs snapshots, and ReadWriteMany works out of the box via NFS.

## Prerequisites

Kubernetes >= 1.30, VolumeSnapshot CRDs + snapshot controller installed (RKE2 includes these out-of-the-box), NFSv4.2 client on all nodes.

## Deploy the CSI Driver

### Helm (Recommended)

```bash
cat > values.yaml <<EOF
storageClasses:
  - name: btrfs-nfs
    nfsServer: "10.0.0.5"             # your agent's IP
    agentURL: "http://10.0.0.5:8080"
    agentToken: "your-tenant-token"   # from agent install
    isDefault: true
EOF

helm install btrfs-nfs-csi oci://ghcr.io/erikmagkekse/charts/btrfs-nfs-csi \
  -n btrfs-nfs-csi --create-namespace -f values.yaml
```

See the [Helm chart README](../../../charts/btrfs-nfs-csi/README.md) for all options.

### Static Manifests

```bash
kubectl apply -f https://raw.githubusercontent.com/erikmagkekse/btrfs-nfs-csi/main/deploy/driver/setup.yaml
# Download storageclass.yaml, edit it: set nfsServer, agentURL, agentToken
# Each StorageClass binds one agent + one tenant (via agentToken secret).
curl -LO https://raw.githubusercontent.com/erikmagkekse/btrfs-nfs-csi/main/deploy/driver/storageclass.yaml
# edit storageclass.yaml
kubectl apply -f storageclass.yaml
```

### Verify

Wait until the controller logs show a successful agent connection:

```
kubectl logs -n btrfs-nfs-csi deploy/btrfs-nfs-csi-controller -c csi-driver
```

```
INF agent healthy - vibes immaculate, bits aligned, absolutely bussin sc=btrfs-nfs version=0.10.0
```

> **Note:** If the agent and driver were built from slightly different commits of the same version, you'll see "agent healthy - commit mismatch" instead. This is normal and everything works fine. Only a WRN-level "version mismatch" indicates a real problem.

**Important: `nfsServer` must be reachable from the same IP that NFS exports are created for.** The node driver resolves a storage IP per node (via `DRIVER_NODE_IP`, `DRIVER_STORAGE_INTERFACE`, or `DRIVER_STORAGE_CIDR`) and tells the agent to create NFS exports for that IP. If the node then connects to the NFS server from a different source IP (e.g. a different network), the mount will fail with "No such file or directory" or not be reachable at all. Make sure `nfsServer` and the node storage IPs are on the same network.

## Use it

```yaml
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: my-data
  annotations:
    btrfs-nfs-csi/compression: "zstd"
spec:
  accessModes: [ReadWriteMany]
  storageClassName: btrfs-nfs
  resources:
    requests:
      storage: 10Gi
```

ReadWriteMany is the default. Every volume is an NFS export.

## Dedicated Storage Network

If your nodes have a separate storage NIC, add to `values.yaml`:

```yaml
driver:
  storageInterface: "eth1"        # by NIC name
  # storageCIDR: "10.10.0.0/24"  # or by subnet
```

## Documentation

| Document | Description |
|---|---|
| [Architecture](architecture.md) | Components, volume lifecycle, ID formats, CSI capabilities, sidecars, RBAC, HA |
| [Configuration](configuration.md) | Environment variables, StorageClass parameters, PVC annotations, labels, secret |
| [Operations](operations.md) | Snapshots, clones, expansion, NFS health, fsGroup via Kubernetes resources |
| [Metrics](metrics.md) | Controller and node Prometheus metrics, PromQL examples |

See also: [Agent configuration](../../configuration.md), [Agent metrics](../../metrics.md)
