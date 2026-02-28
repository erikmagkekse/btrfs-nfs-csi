# Ansible Examples

> **⚠️ Dev / playground only** - these playbooks are meant for quick experimentation and testing. Do not use them for production deployments. These spin up some resources quick and dirty, like not using the k8s module for Ansible.

Ansible playbooks for deploying btrfs-nfs-csi on Hetzner Cloud with a single command.
Creates an agent server (btrfs storage + NFS) and a K8s server (RKE2) with the CSI driver pre-installed.

Uses [Hetzner Cloud](https://www.hetzner.com/cloud/) because it's cheap and servers can be
created and destroyed easily, ideal for testing and I don't care environments.

## Prerequisites

```bash
pip install ansible hcloud
ansible-galaxy collection install hetzner.hcloud
```

## Usage

```bash
export HCLOUD_TOKEN="your-token-here"
ansible-playbook simple.yaml
```

After the playbook finishes, a summary is printed with all connection details,
including the kubeconfig path, dashboard URL and tenant credentials.

```bash
export KUBECONFIG=/tmp/btrfs-nfs-csi-tmp/rke2-kubeconfig
kubectl get nodes
kubectl get sc
```

### Cleanup

```bash
ansible-playbook simple.yaml -e state=absent
```

## Playbooks

| Playbook | Description |
|----------|-------------|
| `simple.yaml` | 1x agent, 1x K8s (single node) - minimal test setup |
| `simple-ha.yaml` | 2x agent (DRBD replication), 1x K8s (single node) - HA storage (TODO) |

## simple.yaml

Runs four roles in order:

| Role | Description |
|------|-------------|
| `ssh` | Generates a temporary SSH key pair and uploads it to Hetzner Cloud |
| `agent` | Creates a server + volume, runs `quickstart-agent.sh` to set up btrfs + NFS + agent |
| `k8s` | Creates a server, installs RKE2, deploys the CSI driver, StorageClass and VolumeSnapshotClass |
| `info` | Verifies the deployment and prints a summary with all connection details |

## Configuration

Override any default variable via `-e`:

```bash
ansible-playbook simple.yaml -e agent_version=0.9.7 -e k8s_server_type=cx32
```

### Agent defaults

| Variable | Default |
|----------|---------|
| `server_type` | `cx23` |
| `server_image` | `debian-13` |
| `volume_size` | `20` (GB) |
| `agent_version` | `0.9.6` |
| `agent_image` | (unset, set by quickstart install script) |
| `agent_base_path` | `/export/data` |

### K8s defaults

| Variable | Default |
|----------|---------|
| `k8s_server_type` | `cx23` |
| `k8s_server_image` | `debian-13` |
| `rke2_channel` | `stable` |
| `driver_image` | (unset, uses image from `setup.yaml`) |

### Dev setup

To test a local build, build and push your image to a registry the servers can reach, then pass it via `-e`:

```bash
# Build and push your dev image
podman build -t ghcr.io/youruser/btrfs-nfs-csi:edge .
podman push ghcr.io/youruser/btrfs-nfs-csi:edge

# Deploy with custom image for both agent and driver
ansible-playbook simple.yaml \
  -e agent_image=ghcr.io/erikmagkekse/btrfs-nfs-csi:edge \
  -e driver_image=ghcr.io/erikmagkekse/btrfs-nfs-csi:edge
```

`agent_image` overrides the container image on the agent host (via quickstart script).
`driver_image` overrides the CSI controller + node driver image in the Kubernetes deployment.
Both are optional and independent - you can override just one if needed.

### Shared defaults

| Variable | Default |
|----------|---------|
| `prefix` | `btrfs-nfs-csi` (used for server/volume names) |
| `ssh_key_name` | `btrfs-nfs-csi` |
