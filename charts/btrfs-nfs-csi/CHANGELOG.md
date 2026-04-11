# Changelog

## 0.2.0

Companion release for appVersion 0.10.0. If upgrading from 0.1.x, see the [Upgrade Guide](../../RELEASE.md#upgrade-guide) for important notes on label backfill and export migration.

- Use `integration kubernetes controller` and `integration kubernetes driver` subcommands
- Add `--extra-create-metadata` to snapshotter sidecar for label propagation
- Default `hostNetwork: true` for NFS4 session stability across driver pod restarts
- Add `DRIVER_DEFAULT_LABELS` example in values for multi-cluster/multi-tenant setups
- Add OCI image annotations to Chart.yaml
- Fix maintainer name

## 0.1.0

Initial Helm chart release.

- Controller Deployment + Driver DaemonSet with all CSI sidecars
- StorageClasses as list for multi-agent/multi-tenant setups
- Secret management via `existingSecret` or inline `agentToken`
- Collision detection and token validation
- PodMonitor support for Prometheus Operator
- Configurable probes, security contexts, RBAC, topology spread
- `extraDeploy` for additional manifests
