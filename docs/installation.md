# Installation

## Prerequisites

**Agent host:** Linux >= 5.15, `btrfs-progs` >= 6.x, `nfs-utils`, mounted btrfs filesystem, root (until NFS-Ganesha support)

**Kubernetes:** >= 1.30, VolumeSnapshot CRDs + snapshot controller installed (RKE2 includes these out-of-the-box), NFSv4.2 client on all nodes

## Agent Setup

### Quick Install (Recommended)

The fastest way to get the agent running. Requires a mounted btrfs filesystem with quotas enabled or a clean block device.

```bash
# The agent runs as a privileged Podman container with host networking -
# it listens on port 8080 and manages the host's NFS exports directly.
#
# Environment variables (defaults shown - adjust as needed):
# export AGENT_BASE_PATH=/export/data  # must be a btrfs filesystem
# export AGENT_TENANTS=default:$(openssl rand -hex 16)
# export AGENT_LISTEN_ADDR=:8080
# export AGENT_BLOCK_DISK=/dev/sdb  # auto-format as btrfs + mount to AGENT_BASE_PATH
# export VERSION=0.9.7
# export IMAGE=ghcr.io/erikmagkekse/btrfs-nfs-csi:0.9.7  # override full image ref
# export SKIP_PACKAGE_INSTALL=1

curl -fsSL https://raw.githubusercontent.com/erikmagkekse/btrfs-nfs-csi/main/scripts/quickstart-agent.sh # | sudo -E bash

# Save the tenant token printed at the end!
```

| Variable | Default | Description |
|----------|---------|-------------|
| `AGENT_BASE_PATH` | `/export/data` | btrfs mount point |
| `AGENT_TENANTS` | `default:<random>` | tenant:token pairs |
| `AGENT_LISTEN_ADDR` | `:8080` | listen address |
| `VERSION` | `0.9.7` | container image tag |
| `IMAGE` | `ghcr.io/erikmagkekse/btrfs-nfs-csi:<VERSION>` | full container image reference (overrides `VERSION`) |
| `AGENT_BLOCK_DISK` | (unset) | block device to auto-format as btrfs and mount (e.g. `/dev/sdb`) |
| `SKIP_PACKAGE_INSTALL` | (unset) | set to `1` to skip package installation |

The script installs prerequisites (podman, NFS server, btrfs-progs), generates a config file, sets up a Podman Quadlet, and starts the service.

**Update:** Run the same command again to update. The script detects an existing installation, preserves your tenant config, updates the container image + Quadlet file, and restarts the service. Pass `--yes` / `-y` to skip the confirmation prompt (e.g. for CI).

**Uninstall:** Removes config and Quadlet file but keeps your data.

```bash
curl -fsSL https://raw.githubusercontent.com/erikmagkekse/btrfs-nfs-csi/main/scripts/quickstart-agent.sh | sudo -E bash -s -- --uninstall
```

### Manual Setup

<details>
<summary>Step-by-step manual installation</summary>

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
AGENT_TENANTS=default:$(openssl rand -hex 16)
AGENT_LISTEN_ADDR=:8080
EOF
chmod 600 /etc/btrfs-nfs-csi/agent.env

systemctl daemon-reload  # Quadlet generator creates the service, autostart via [Install] WantedBy=multi-user.target
systemctl start btrfs-nfs-csi-agent
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

</details>

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
INF agent healthy - vibes immaculate, bits aligned, absolutely bussin agent=http://10.0.1.100:8080 version=0.9.7
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
