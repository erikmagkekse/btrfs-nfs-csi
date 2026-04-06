# Integrations

The agent exposes a REST API. Any system that can make HTTP calls can manage volumes, snapshots, and exports. The CLI and all integrations use the same API.

| Integration | Status | Documentation |
|---|---|---|
| [Kubernetes (CSI Driver)](kubernetes/) | Beta | PVCs, VolumeSnapshots, ReadWriteMany via NFS |
| Nomad | Idea | CSI plugin for HashiCorp Nomad |
| Docker | Idea | `docker volume create` support |
| Proxmox | Idea | Storage plugin for Proxmox VE |

Want to build an integration? [Open an issue](https://github.com/erikmagkekse/btrfs-nfs-csi/issues) or submit a PR.
