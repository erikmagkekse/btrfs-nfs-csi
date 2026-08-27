# Release v0.11.3

**Previous: v0.11.2** | **Date: 2026-08-27**

Routine dependency refresh across the Kubernetes client family, gRPC, echo/v5, `golang.org/x/*`, and Prometheus client. Container build and runtime stages now pinned by SHA256 digest to `golang:1.26.7-alpine3.24` and `alpine:3.24` respectively. No behavior, manifest, or API changes.

> **Successor:** Active development moves to a hard fork named **[ButterStore](https://github.com/butterstore)**, which reframes the project around what it has actually become: a general-purpose btrfs storage backend where the Kubernetes CSI driver is one of several integrations. Migration is a drop-in: tokens, REST API, CLI, Helm values, StorageClasses, PVCs, and VolumeSnapshots all keep working. A migration guide ships with the first ButterStore release. Once ButterStore ships, this repo gets archived. Published artifacts (container images, Helm charts) stay available.
>
> *Coming in the first ButterStore release:* snapshot streaming through `snapshot send` / `snapshot receive`. Dump a snapshot to a file, feed one back from a file, or pipe the two commands together to move a snapshot between agents. Auth is capability-token-based, rate limits are configurable globally, per tenant, and per task, and transfers are tracked as tasks that resume from an offset header if the connection drops, so a flaky network doesn't restart the whole thing. Scheduled replication comes later.
>
> *The name plays on the colloquial pronunciation of btrfs as "butter-FS" plus its role as a storage backend.*

---

## Security notes

- **Go stdlib (container base pinned to 1.26.7)**: CVE-2026-56862 (`crypto/tls` KeyUpdate DoS), CVE-2026-56853 (`net/http` ReadHeaderTimeout bypass on HTTP/2 preface), CVE-2026-56860 (`net/url`), CVE-2026-56859 (`encoding/xml`), CVE-2026-56858 (`html/template`), CVE-2026-56864 / CVE-2026-56865 (`cmd/go` GOSUMDB/GOPROXY bypass).
- **google.golang.org/grpc 1.81.0 → 1.83.2**: past the 1.82.1 fix for GHSA-hrxh-6v49-42gf / GO-2026-6061 (server-side HTTP/2 transport + xDS RBAC).
- **github.com/labstack/echo/v5 5.1.1 → 5.3.1**: past the 5.2.0 fix for CVE-2026-55677 (encoded-slash bypass of route-level middleware when a broader static-file root sits under a protected prefix; not reachable from this codebase).
- **golang.org/x/mod 0.38.0 → 0.40.0**: GO-2026-6180 / CVE-2026-56864 (`cmd/go` GOSUMDB bypass) and GO-2026-6179 / CVE-2026-56865 (`sumdb/tlog` transparency-log tile verification bypass). Also lifts `golang.org/x/tools` to 0.49.0.
- **golang.org/x/crypto 0.51.0 → 0.55.0**: rolls through the June 2026 x/crypto advisory batch.

---

## Dependencies

- Bump `k8s.io/api`, `k8s.io/apimachinery`, `k8s.io/client-go`, `k8s.io/mount-utils` from 0.36.0 to 0.36.4
- Bump `github.com/kubernetes-csi/external-snapshotter/client/v8` from 8.4.0 to 8.6.0
- Bump `github.com/container-storage-interface/spec` from 1.12.0 to 1.13.0
- Bump `google.golang.org/protobuf` to 1.36.12 (out of pre-release)
- Bump `golang.org/x/sys` from 0.44.0 to 0.47.0
- Bump `golang.org/x/term` from 0.43.0 to 0.45.0
- Bump `github.com/prometheus/client_golang` from 1.23.2 to 1.24.1
- Bump `github.com/stretchr/testify` from 1.11.1 to 1.12.1
- Bump `github.com/urfave/cli/v3` from 3.8.0 to 3.11.0
- Refresh remaining indirect dependencies

---

## Upgrade Guide

Drop-in from v0.11.2. Bump the image tag to `0.11.3`. No manifest, RBAC, StorageClass, config, or CLI changes.

Operators rebuilding from source: `go.mod` requires Go `>=1.26.5`. The `Containerfile` pins both build (`golang:1.26.7-alpine3.24`) and runtime (`alpine:3.24`) stages by SHA256 digest for reproducible builds.

---

## Packaging

- Container image OCI annotations now render on the ghcr.io package page (title, description, source, license).
- `image.vendor` / `image.authors` split for OCI convention.

---

## Deprecations

None.

---

*Cut aboard Lufthansa flight LH717.*
