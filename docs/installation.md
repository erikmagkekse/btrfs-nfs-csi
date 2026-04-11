# Installation

## Prerequisites

Linux >= 5.15, `btrfs-progs` >= 6.x, `nfs-utils`, mounted btrfs filesystem, root

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
# export AGENT_BLOCK_DISK=/dev/sdX  # optional, auto-format as btrfs + mount to AGENT_BASE_PATH 
# export VERSION=0.10.0
# export IMAGE=ghcr.io/erikmagkekse/btrfs-nfs-csi:0.10.0  # override full image ref
# export SKIP_PACKAGE_INSTALL=1

curl -fsSL https://raw.githubusercontent.com/erikmagkekse/btrfs-nfs-csi/main/scripts/quickstart-agent.sh # | sudo -E bash

# Save the tenant token printed at the end!
```

| Variable | Default | Description |
|----------|---------|-------------|
| `AGENT_BASE_PATH` | `/export/data` | btrfs mount point |
| `AGENT_TENANTS` | `default:<random>` | tenant:token pairs |
| `AGENT_LISTEN_ADDR` | `:8080` | listen address |
| `VERSION` | `0.10.0` | container image tag |
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
# simple quotas (squota) -- recommended, requires kernel 6.7+ and btrfs-progs 6.7+
btrfs quota enable -s /export/data

# classic quotas -- fallback for older kernels
# btrfs quota enable /export/data
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

To build from source: `CGO_ENABLED=0 go build -o btrfs-nfs-csi ./cmd/btrfs-nfs-csi`

### 3c. NixOS

This is an example working flake:

```nix
{
  inputs = {
    ...
    btrfs-nfs-csi.url = "github:erikmagkekse/btrfs-nfs-csi";
  };

  outputs = {
    nixpkgs,
    ...,
    btrfs-nfs-csi
  }: {
    nixosConfigurations."demo" = nixpkgs.lib.nixosSystem {
      system = "x86_64-linux";
      modules = [
        btrfs-nfs-csi.nixosModules.btrfs-nfs-csi
        {
          services.btrfs-nfs-csi.agent.example = {
            basePath = "/export/data";
            listenAddr = ":8080";
            metricsAddr = "127.0.0.1:9090";

            environmentFile = ./envfile.env;
          };
        }
      ];
    };
  };
}
```

WARNING: The NixOS module does not read from ``/etc/btrfs-nfs-csi``, you need to specify the configuration file as an option.

To hide environment secrets from the store, I suggest using something like [sops-nix](https://github.com/Mic92/sops-nix).

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

For multiple tenants on one agent:

```bash
AGENT_TENANTS=cluster-a:token-aaa,cluster-b:token-bbb
```

Each tenant is isolated (separate directory, separate token). See [multi-tenancy](architecture.md#multi-tenancy) for details.

</details>

## Integrations

With the agent running, deploy an integration to connect your workload orchestrator. See [Integrations](integrations/) for available options.

For Kubernetes: [Deploy the CSI driver](integrations/kubernetes/)
