# Changelog

## 0.1.0

Initial Helm chart release for btrfs-nfs-csi.

### Features

- Controller Deployment with CSI sidecars (provisioner, attacher, snapshotter, resizer, liveness-probe)
- Driver DaemonSet with node-driver-registrar and liveness-probe
- StorageClasses as list for multi-agent/multi-tenant setups
- VolumeSnapshotClass creation per StorageClass (optional)
- Secret management via `existingSecret` (recommended) or inline `agentToken`
- Dedicated storage network support (`storageInterface` / `storageCIDR`)
- Configurable RBAC with `extraRules`
- Per-container customization: `securityContext`, `extraArgs`, `extraEnv`, `extraVolumeMounts`, `resources`
- `extraDeploy` for additional manifests (SealedSecrets, ServiceMonitors, etc.)
- Configurable `kubeletDir` for alternative K8s distributions
