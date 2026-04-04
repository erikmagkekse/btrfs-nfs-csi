# Release Process

## Branches

| Branch | Purpose |
|--------|---------|
| `main` | Stable releases |
| `edge` | Development, pre-release testing |

## CI Pipeline

PRs against `main` or `edge` run the CI workflow:

```
ci.yml (Go code, Containerfile, VERSION changes)
  lint         format check + golangci-lint
  test         go test -race ./...
  integration  btrfs + NFS integration tests (needs: test)
  sanity       CSI sanity tests (planned, not yet implemented)
  e2e          end-to-end tests (planned, not yet implemented)
  build        container image build check, no push (needs: test + integration)

ci-helm.yml (charts/** changes)
  lint         helm lint + helm template
```

When a PR is merged to `edge`, the edge-build workflow pushes the `:edge` image.

## Artifacts

| Event | Container Image | Helm Chart |
|-------|----------------|------------|
| PR to `edge`/`main` | build check (no push) | lint only |
| PR merged to `edge` | `:edge` (rolling, amd64) | not published |
| Tag `v0.10.0-edge` on `edge` | `:0.10.0-edge` (amd64 + arm64) | `0.1.2-edge` |
| Tag `v0.10.0` on `main` | `:0.10.0` (amd64 + arm64) | `0.1.2` |

Container images: `ghcr.io/erikmagkekse/btrfs-nfs-csi`
Helm charts: `oci://ghcr.io/erikmagkekse/charts/btrfs-nfs-csi`

## Creating a Release

### 1. Bump versions

Three files must be updated together:

| File | Field | Example |
|------|-------|---------|
| `VERSION` | app version | `0.10.0` |
| `charts/btrfs-nfs-csi/Chart.yaml` | `appVersion` | `"0.10.0"` |
| `charts/btrfs-nfs-csi/Chart.yaml` | `version` | `0.1.2` |

If `go.mod` or `go.sum` changed since the last release, also update `vendorHash` in `package.nix`:

```bash
# Build with nix to get the new hash (it will fail and print the expected hash)
nix build .# 2>&1 | grep 'got:' | awk '{print $2}'
# Update package.nix with the new hash
```

### 2. Tag and push

**Stable release** (from `main`):

```bash
git tag v0.10.0
git push origin v0.10.0
```

**Edge pre-release** (from `edge`):

```bash
git tag v0.10.0-edge
git push origin v0.10.0-edge
```

### 3. Automated checks

The release workflow runs a `check` job before building. It verifies:

- Tag matches the `VERSION` file (with `-edge` suffix for edge tags)
- `VERSION` is greater than the previous release
- Chart `version` is greater than the previous release
- Chart `appVersion` matches `VERSION`
- If `go.mod`/`go.sum` changed: `vendorHash` in `package.nix` has been updated

If any check fails, the build and Helm publish are skipped.

### 4. Release pipeline

```
release.yml (on tag v*)
  check    version validation + bump verification
  build    container image (needs: check, parallel with helm)
  helm     chart package + push (needs: check, parallel with build)
```

Edge and stable releases use the same pipeline. The `-edge` suffix is detected automatically and applied to image tags and chart versions.

## Version Scheme

- App version (`VERSION`): semver, e.g. `0.10.0`
- Chart version (`Chart.yaml` `version`): independent semver for Helm chart changes
- Edge builds append `-edge` to both, e.g. `0.10.0-edge`, `0.1.2-edge`
- The `:edge` container tag is a rolling tag updated on every PR merge to `edge`
