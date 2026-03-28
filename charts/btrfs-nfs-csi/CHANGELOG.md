# Changelog

## 0.1.0

Initial Helm chart release.

- Controller Deployment + Driver DaemonSet with all CSI sidecars
- StorageClasses as list for multi-agent/multi-tenant setups
- Secret management via `existingSecret` or inline `agentToken`
- Collision detection and token validation
- PodMonitor support for Prometheus Operator
- Configurable probes, security contexts, RBAC, topology spread
- `extraDeploy` for additional manifests
