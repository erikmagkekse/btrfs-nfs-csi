# btrfs-nfs-csi Helm Chart

Kubernetes CSI driver that turns any btrfs filesystem into a full-featured NFS storage backend with instant snapshots, clones, and quotas.

## Prerequisites

- Kubernetes >= 1.30
- VolumeSnapshot CRDs + snapshot controller installed (rke2 already has it per default)
- NFSv4.2 client on all nodes
- At least one [btrfs-nfs-csi agent](https://github.com/erikmagkekse/btrfs-nfs-csi) running on a host

## Install

```bash
helm install btrfs-nfs-csi oci://ghcr.io/erikmagkekse/charts/btrfs-nfs-csi \
  -n btrfs-nfs-csi --create-namespace \
  -f values.yaml
```

## Minimal values.yaml

```yaml
storageClasses:
  - name: btrfs-nfs
    nfsServer: "10.0.0.5"
    agentURL: "http://10.0.0.5:8080"
    existingSecret: "btrfs-nfs-creds"
    isDefault: true
```

## Multiple agents

```yaml
storageClasses:
  - name: btrfs-nfs
    nfsServer: "10.0.0.5"
    agentURL: "http://10.0.0.5:8080"
    existingSecret: "agent-1-creds"
    isDefault: true

  - name: btrfs-nfs-backup
    nfsServer: "10.0.0.6"
    agentURL: "http://10.0.0.6:8080"
    existingSecret: "agent-2-creds"
    reclaimPolicy: Retain
```

## Dedicated storage network

If your nodes have a separate NIC or subnet for storage traffic:

```yaml
driver:
  storageInterface: "eth1"    # or storageCIDR: "10.10.0.0/24"
```

`hostNetwork` is auto-enabled when either is set.

## Values

### Global

| Key | Default | Description |
|-----|---------|-------------|
| `driverName` | `btrfs-nfs-csi` | CSI driver name (do not change after volumes exist) |
| `logLevel` | `info` | Log level: debug, info, warn, error |
| `image.repository` | `ghcr.io/erikmagkekse/btrfs-nfs-csi` | Driver image |
| `image.tag` | appVersion | Image tag |
| `image.pullPolicy` | `IfNotPresent` | Pull policy |
| `imagePullSecrets` | `[]` | Registry secrets |
| `nameOverride` | `""` | Override chart name |
| `fullnameOverride` | `""` | Override full release name |
| `kubeletDir` | `/var/lib/kubelet` | Kubelet directory path |
| `extraDeploy` | `[]` | Extra manifests (SealedSecrets, ServiceMonitors, etc.) |

### RBAC

| Key | Default | Description |
|-----|---------|-------------|
| `rbac.create` | `true` | Create ClusterRoles and ClusterRoleBindings |
| `rbac.controllerExtraRules` | `[]` | Extra rules for controller ClusterRole |
| `rbac.driverExtraRules` | `[]` | Extra rules for driver ClusterRole |

### Service Accounts

| Key | Default | Description |
|-----|---------|-------------|
| `serviceAccount.controller.create` | `true` | Create controller SA |
| `serviceAccount.controller.automount` | `true` | Automount API token |
| `serviceAccount.controller.annotations` | `{}` | SA annotations (e.g. IRSA) |
| `serviceAccount.controller.name` | `""` | Override SA name |
| `serviceAccount.driver.*` | _(same as controller)_ | Driver SA settings |

### Controller (Deployment)

| Key | Default | Description |
|-----|---------|-------------|
| `controller.replicas` | `1` | Replica count |
| `controller.metricsAddr` | `:9090` | Metrics listen address |
| `controller.priorityClassName` | `system-cluster-critical` | Priority class |
| `controller.podSecurityContext` | `{}` | Pod security context |
| `controller.securityContext` | `{readOnlyRootFilesystem: true, runAsNonRoot: true, runAsUser: 65534}` | Container security context |
| `controller.resources` | 10m CPU, 32Mi/128Mi memory | Resource requests/limits |
| `controller.tolerations` | control-plane, master | Tolerations |
| `controller.nodeSelector` | `{}` | Node selector |
| `controller.affinity` | `{}` | Affinity rules |
| `controller.podAnnotations` | `{}` | Pod annotations |
| `controller.podLabels` | `{}` | Extra pod labels |
| `controller.livenessProbe` | httpGet /healthz:9808 | Liveness probe (fully customizable) |
| `controller.readinessProbe` | `{}` | Readiness probe (disabled by default) |
| `controller.topologySpreadConstraints` | `[]` | Topology spread for HA with replicas > 1 |
| `controller.extraArgs` | `[]` | Extra args for csi-driver container |
| `controller.extraEnv` | `[]` | Extra env vars |
| `controller.extraVolumes` | `[]` | Extra volumes |
| `controller.extraVolumeMounts` | `[]` | Extra volume mounts |

### Controller Sidecars

Each sidecar under `controller.sidecars.<name>` supports:

| Key | Description |
|-----|-------------|
| `.image.repository` | Image repository |
| `.image.tag` | Image tag |
| `.image.pullPolicy` | Pull policy |
| `.securityContext` | Container security context |
| `.extraArgs` | Extra arguments |
| `.extraEnv` | Extra environment variables |
| `.extraVolumeMounts` | Extra volume mounts |
| `.resources` | Resource requests/limits |

Sidecars: `provisioner`, `attacher`, `snapshotter`, `resizer`, `livenessProbe`

### Driver (DaemonSet)

| Key | Default | Description |
|-----|---------|-------------|
| `driver.metricsAddr` | `:9090` | Metrics listen address |
| `driver.priorityClassName` | `system-node-critical` | Priority class |
| `driver.storageInterface` | `""` | Dedicated storage NIC |
| `driver.storageCIDR` | `""` | Storage subnet CIDR |
| `driver.hostNetwork` | `false` | Host networking (auto-enabled for storage NIC/CIDR) |
| `driver.updateStrategy` | RollingUpdate, maxUnavailable 1 | DaemonSet update strategy |
| `driver.podSecurityContext` | `{}` | Pod security context |
| `driver.resources` | 10m CPU, 32Mi/128Mi memory | Resource requests/limits |
| `driver.tolerations` | control-plane, master, NoExecute, NoSchedule | Tolerations |
| `driver.nodeSelector` | `{}` | Node selector |
| `driver.affinity` | `{}` | Affinity rules |
| `driver.podAnnotations` | `{}` | Pod annotations |
| `driver.podLabels` | `{}` | Extra pod labels |
| `driver.livenessProbe` | httpGet /healthz:9808 | Liveness probe (fully customizable) |
| `driver.readinessProbe` | `{}` | Readiness probe (disabled by default) |
| `driver.extraArgs` | `[]` | Extra args for csi-driver container |
| `driver.extraEnv` | `[]` | Extra env vars |
| `driver.extraVolumes` | `[]` | Extra volumes |
| `driver.extraVolumeMounts` | `[]` | Extra volume mounts |

Driver sidecars (`nodeDriverRegistrar`, `livenessProbe`) support the same fields as controller sidecars.

> **Note:** The driver container always runs as `privileged: true` (required for mount operations).

### StorageClasses

Each entry in `storageClasses[]` creates a Secret, StorageClass, and optionally a VolumeSnapshotClass.

| Key | Required | Default | Description |
|-----|----------|---------|-------------|
| `name` | yes | | StorageClass name (immutable, part of volume IDs) |
| `nfsServer` | yes | | NFS server IP |
| `agentURL` | yes | | Agent REST API URL |
| `existingSecret` | | | Pre-existing Secret with `agentToken` key (recommended) |
| `agentToken` | | | Agent token (chart creates Secret, plain text in values) |
| `nfsMountOptions` | | | NFS mount options |
| `nocow` | | | `"true"` or `"false"` |
| `compression` | | | `zstd`, `lzo`, `zlib`, `zstd:3`, `none` |
| `uid` | | | Volume owner UID |
| `gid` | | | Volume owner GID |
| `mode` | | | Octal permissions, e.g. `"2770"` |
| `reclaimPolicy` | | `Delete` | `Delete` or `Retain` |
| `allowVolumeExpansion` | | `true` | Allow PVC resize |
| `volumeBindingMode` | | `Immediate` | `Immediate` or `WaitForFirstConsumer` |
| `labels` | | `{}` | Extra labels on the StorageClass |
| `annotations` | | `{}` | Extra annotations on the StorageClass |
| `isDefault` | | `false` | Set as default StorageClass |
| `snapshotClass` | | `true` | Create a VolumeSnapshotClass |
| `snapshotClassLabels` | | `{}` | Extra labels on the VolumeSnapshotClass |
| `snapshotClassAnnotations` | | `{}` | Extra annotations on the VolumeSnapshotClass |
| `snapshotDeletionPolicy` | | `Delete` | `Delete` or `Retain` |

### Metrics

| Key | Default | Description |
|-----|---------|-------------|
| `metrics.podMonitor.enabled` | `false` | Create PodMonitors for Prometheus Operator |
| `metrics.podMonitor.labels` | `{}` | Extra labels (e.g. `release: kube-prometheus-stack`) |
| `metrics.podMonitor.annotations` | `{}` | Extra annotations |
| `metrics.podMonitor.interval` | | Scrape interval |
| `metrics.podMonitor.scrapeTimeout` | | Scrape timeout |

### Validation

The chart includes built-in safety checks:

- **Token validation**: Fails if a StorageClass has neither `existingSecret` nor `agentToken` set
- **Collision detection**: Fails if a StorageClass or CSIDriver name is already owned by a different Helm release (via `lookup`)

## Uninstall

```bash
helm uninstall btrfs-nfs-csi -n btrfs-nfs-csi
```

> **Warning:** StorageClasses and VolumeSnapshotClasses are cluster-scoped but managed by Helm. They will be deleted on uninstall. Existing PVs are not affected.
