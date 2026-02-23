# Installation

## Prerequisites

**Agent host:** Linux >= 5.15, `btrfs-progs` >= 6.x, `nfs-utils`, mounted btrfs filesystem, root (until NFS-Ganesha support)

**Kubernetes:** >= 1.30, VolumeSnapshot CRDs + snapshot controller installed (RKE2 includes these out-of-the-box), NFSv4.2 client on all nodes

## Agent Setup

### 1. btrfs Filesystem

```bash
apt install btrfs-progs   # Debian/Ubuntu

# Find your disk
lsblk -f

mkfs.btrfs /dev/sdX
mkdir -p /export/data
mount /dev/sdX /export/data
btrfs quota enable /export/data
```

Add to `/etc/fstab` (use UUID for stability):

```bash
UUID=$(blkid -s UUID -o value /dev/sdX)
echo "UUID=$UUID  /export/data  btrfs  defaults  0  0" >> /etc/fstab
```

### 2. NFS Server

```bash
apt install nfs-kernel-server   # Debian/Ubuntu
systemctl enable --now nfs-server
```

No manual `/etc/exports` configuration needed - the agent manages NFS exports automatically via `exportfs`.

### 3a. Podman Quadlet (Recommended)

```bash
curl -Lo /etc/containers/systemd/btrfs-nfs-csi-agent.container \
  https://raw.githubusercontent.com/erikmagkekse/btrfs-nfs-csi/main/deploy/agent/btrfs-nfs-csi-agent.container
```

### 3b. Binary

```bash
cp btrfs-nfs-csi /usr/local/bin/
chmod +x /usr/local/bin/btrfs-nfs-csi
curl -Lo /etc/systemd/system/btrfs-nfs-csi-agent.service \
  https://raw.githubusercontent.com/erikmagkekse/btrfs-nfs-csi/main/deploy/agent/agent.service
```

To build from source: `CGO_ENABLED=0 go build -o btrfs-nfs-csi .`

### 4. Configure and Start

```bash
install -d -m 700 /etc/btrfs-nfs-csi
cat > /etc/btrfs-nfs-csi/agent.env <<EOF
AGENT_BASE_PATH=/export/data
AGENT_TENANTS=default:$(openssl rand -hex 16) # tenantName:token,tenant2:pass
AGENT_LISTEN_ADDR=:8080
EOF
chmod 600 /etc/btrfs-nfs-csi/agent.env

systemctl daemon-reload
systemctl enable --now btrfs-nfs-csi-agent
```

Verify:

```bash
curl http://localhost:8080/healthz
```

For multiple clusters/tenants on one agent:

```bash
AGENT_TENANTS=cluster-a:token-aaa,cluster-b:token-bbb
```

Each tenant maps to one Kubernetes StorageClass. The StorageClass references the agent via `agentURL` and the tenant via `agentToken` in a Secret.

## Driver Setup

```bash
kubectl apply -f https://raw.githubusercontent.com/erikmagkekse/btrfs-nfs-csi/main/deploy/driver/setup.yaml
# Download storageclass.yaml, edit it: set nfsServer, agentURL, agentToken
# Each StorageClass binds one agent + one tenant (via agentToken secret).
curl -LO https://raw.githubusercontent.com/erikmagkekse/btrfs-nfs-csi/main/deploy/driver/storageclass.yaml
# edit storageclass.yaml
kubectl apply -f storageclass.yaml
```

**Important: `nfsServer` must be reachable from the same IP that NFS exports are created for.** The node driver resolves a storage IP per node (via `DRIVER_NODE_IP`, `DRIVER_STORAGE_INTERFACE`, or `DRIVER_STORAGE_CIDR`) and tells the agent to create NFS exports for that IP. If the node then connects to the NFS server from a different source IP (e.g. a different network), the mount will fail with "No such file or directory" or not be reachable at all. Make sure `nfsServer` and the node storage IPs are on the same network.

Wait until the controller logs show a successful agent connection:

```
kubectl logs -n btrfs-nfs-csi deploy/btrfs-nfs-csi-controller -c csi-driver
```

```
INF agent healthy - vibes immaculate, bits aligned, absolutely bussin agent=http://10.0.1.100:8080 version=0.9.5
```

## Use it

```yaml
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: my-data
  annotations:
    btrfs-nfs-csi/compression: "zstd"   # optional
    btrfs-nfs-csi/nocow: "false"        # optional
    btrfs-nfs-csi/uid: "1000"           # optional
    btrfs-nfs-csi/gid: "1000"           # optional
    btrfs-nfs-csi/mode: "0750"          # optional
spec:
  accessModes: [ReadWriteOnce] # of course supports ReadWriteMany
  storageClassName: btrfs-nfs
  resources:
    requests:
      storage: 10Gi
```

See [operations.md](operations.md) for snapshots, clones, expansion, and more.
