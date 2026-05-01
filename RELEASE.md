# Release v0.11.1

**Previous: v0.11.0** | **Date: 2026-05-01**

Per-request audit access log with caller identity, plus a logging hardening pass that closes a token-leak path and surfaces previously silent errors. CLI default list filter now follows `AGENT_CSI_IDENTITY`. No breaking changes.

> **`btrfs-nfs-csi` is being deprecated, ButterStore is the successor.** Active development moves to a hard fork named **ButterStore**, which reframes the project around what it has actually become: a general-purpose btrfs storage backend where the Kubernetes CSI driver is one of several integrations. Migration is a drop-in: tokens, REST API, CLI, Helm values, StorageClasses, PVCs, and VolumeSnapshots all keep working. A migration guide ships with the first ButterStore release. Until then, this repo gets a few more releases, then archive.

---

## Highlights

### Per-request audit access log

Every API call produces one access log line with full caller identity:

```
INF request method=POST path=/v1/volumes code=201 took=9.4ms tenant=ops role=user identity=ci-bot token_fingerprint=ab12...e1f4 client=10.0.0.5 user_agent=btrfs-nfs-csi-cli/0.11.1
```

Storage events inherit the same caller fields. Level by status: 5xx → error, 4xx → warn, 2xx/3xx → info. `/healthz` is skipped. To trace every action a token took: `grep token_fingerprint=ab12 access.log`.

### Default list filter follows the active identity

The CLI used to hardcode `created-by=cli` regardless of `AGENT_CSI_IDENTITY`. It now follows the configured identity. `--all` / `-A` still bypasses.

---

## Security

- Auth tokens can no longer leak into logs at any level (#161).
- Truncated root secret file is rejected at startup instead of producing predictable subkeys (#161).

## Bug Fixes

- `AGENT_CSI_IDENTITY` honoured in default list filter (#158).
- 4xx/5xx HTTP responses now log at the right level and are counted under the right Prometheus code label (#160).

## Improvements

- CodeQL path-injection false positives silenced (#157).
- Per-request access log with caller identity (#160). See Highlights.
- Failed immutable-bit operations on metadata files now log a warning (#161).
- Task cleanup retries failed file deletions instead of leaving ghost tasks behind (#161).
- Initialisation, listen-bind, and server-runtime failures shut down through the normal error path instead of `os.Exit` from inside library code (#161).
- Failed rollback of half-created volumes, snapshots, or clones is logged (#161).

## Documentation

- Quickstart curl typo fixed.
- Task system architecture page brought up to date.

## Dependencies

- Bump `github.com/rs/zerolog` from 1.35.0 to 1.35.1 (#145)

---

## Upgrade Guide

Drop-in from v0.11.0. Bump the image tag to `0.11.1`.

- **Kubernetes CSI users:** no action required.
- **CLI users with a custom identity:** drop any `--all` workaround, the default now matches your identity.
- **Operators / log shippers:** expect one info-level access log line per authenticated API call. `/healthz` polling does not log. Set `LOG_LEVEL=warn` to silence the success path.

---

## Deprecations

None.
