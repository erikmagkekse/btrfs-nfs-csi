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
# export VERSION=0.11.2
# export IMAGE=ghcr.io/erikmagkekse/btrfs-nfs-csi:0.11.2  # override full image ref
# export SKIP_PACKAGE_INSTALL=1

curl -fsSL https://raw.githubusercontent.com/erikmagkekse/btrfs-nfs-csi/main/scripts/quickstart-agent.sh | sudo -E bash

# Save the tenant token printed at the end!
```

| Variable | Default | Description |
|----------|---------|-------------|
| `AGENT_BASE_PATH` | `/export/data` | btrfs mount point |
| `AGENT_TENANTS` | `default:<random>` | tenant:token pairs |
| `AGENT_LISTEN_ADDR` | `:8080` | listen address |
| `VERSION` | `0.11.2` | container image tag |
| `IMAGE` | `ghcr.io/erikmagkekse/btrfs-nfs-csi:<VERSION>` | full container image reference (overrides `VERSION`) |
| `AGENT_BLOCK_DISK` | (unset) | block device to auto-format as btrfs and mount (e.g. `/dev/sdb`) |
| `SKIP_PACKAGE_INSTALL` | (unset) | set to `1` to skip package installation |

The script installs prerequisites (podman, NFS server, btrfs-progs), generates a config file, sets up a Podman Quadlet, and starts the service.

**Supported distributions.** The quickstart needs Podman 4.4 or newer because the agent ships as a Quadlet container. Tested on Debian 13, Ubuntu 24.04, openSUSE Leap 15/16, and Fedora 42/43. Older Debian/Ubuntu releases (Debian 11/12, Ubuntu 22.04) ship Podman 3.x or 4.3, which lack Quadlet, install a newer Podman from your distro's backports or upstream repo first, then re-run with `SKIP_PACKAGE_INSTALL=1`. RHEL-family distros (RHEL/Alma/Rocky/CentOS Stream) need `btrfs-progs` from EPEL and a kernel that still has the btrfs module, RHEL 10 dropped btrfs from the kernel entirely so it cannot be used. Arch and NixOS work via their own packaging paths (see [`flake.nix`](https://github.com/erikmagkekse/btrfs-nfs-csi/blob/main/flake.nix) for NixOS).

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
# simple quotas (squota), recommended, requires kernel 6.7+ and btrfs-progs 6.7+
btrfs quota enable -s /export/data

# classic quotas, fallback for older kernels
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

Each tenant is isolated (separate directory, separate token). Reserved names that cannot be used as tenants: `tasks`, `snapshots`, `data`, `metadata`. See [multi-tenancy](architecture.md#multi-tenancy) for details.

For deployments where `AGENT_TENANTS` exposure is a concern (env-var leak, container config, systemd unit), the token field accepts a bcrypt hash instead of plaintext. Generate one with `btrfs-nfs-csi hash-token` and paste it into the config. Clients still send the plaintext as bearer. See [rbac.md#hashed-tokens](rbac.md#hashed-tokens).

</details>

## Persistent Secret

On first start the agent generates a 512-byte secret at `${AGENT_BASE_PATH}/metadata/root_secret` (file mode `0600`, directory mode `0700`), plus a `root_secret.bak` copy next to it. This secret is what makes token fingerprints stable across restarts: the same token always shows the same fingerprint as long as the secret is the same. Today the secret is only used for token fingerprints (the values shown by `btrfs-nfs-csi whoami` and `btrfs-nfs-csi tokens`). Future features that need agent-local cryptography will use it without any extra configuration.

Stable fingerprints across restarts let you correlate audit logs with a configured token over time. See [rbac.md](rbac.md#token-fingerprints).

**Backup.** Snapshot the entire `${AGENT_BASE_PATH}/metadata/` directory as part of your DR plan. The contents are sensitive material, treat them like a private key.

**Recovery.**

- Primary lost, backup intact: agent rewrites the primary from the backup on next start.
- Backup lost, primary intact: agent rewrites the backup on next start.
- Both lost: a new secret is generated, all token fingerprints change. Tokens themselves keep working (`AGENT_TENANTS` is unchanged), but historical fingerprints in audit logs no longer match. To preserve fingerprint continuity, restore from backup before restart.
- Primary and backup mismatch: startup aborts with a clear error. Inspect both, remove the stale one (or restore from backup), then restart.

## Integrations

With the agent running, deploy an integration to connect your workload orchestrator. See [Integrations](integrations/) for available options.
