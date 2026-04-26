# RBAC

Imagine one agent serving 2 systems, a Kubernetes cluster plus a nightly capacity report. The cluster's CSI controller creates volumes, its node plugins mount them, and the capacity cronjob walks the API. You don't want the report's token to be able to delete volumes.

That's what the three-level `AGENT_TENANTS` format is for. Each level adds one field and one guarantee. Start at Level 1, go further when you hit a concrete need. Tokens in the same config can sit on different levels.

| Level | Format                        | What it gives you                     |
|-------|-------------------------------|---------------------------------------|
| 1     | `name:token`                  | authentication + tenant scoping       |
| 2     | `name:token:role`             | + role-based endpoint gate              |
| 3     | `name:token:role:identity`    | + per-identity ownership of resources   |

## Level 1: `name:token`

```
AGENT_TENANTS=default:s3cret
```

- Authenticates the caller as tenant `default`.
- Each tenant has its own directory tree. Tenants cannot see each other's data, except Tasks.
- Role defaults to `admin`, so every endpoint is reachable.
- `created-by` is not enforced for ownership. Any token can change any resource in the tenant.

Good for single-tenant setups or small homelabs.

## Level 2: `name:token:role`

```
AGENT_TENANTS=ops:tok-ops:admin,ci:tok-ci:user,dash:tok-dash:readonly,node1:tok-n1:mounter
```

Each token now has a role that limits what it can do.

| Role       | Typical holder                                   |
|------------|--------------------------------------------------|
| `admin`    | operators, the CSI controller, Ansible plays     |
| `user`     | CI jobs, the CLI, application code               |
| `mounter`  | CSI node plugins, NFS client sidecars            |
| `readonly` | capacity reports, backup auditors, usage scans   |

Exact allowed endpoints per role are in the [matrix below](#per-endpoint-matrix).

Multiple tokens can share a tenant name with different roles. You'd use that to split duties within one tenant: an `admin` or `user` token for day-to-day work, plus a `readonly` token for audits, or `mounter` tokens for any apps that should only attach exports.

What this still doesn't give you: isolation *within* a tenant. Two `user` tokens in the same tenant can both create and delete each other's volumes. If you need that, go to Level 3.

## Level 3: `name:token:role:identity`

```
AGENT_TENANTS=ci:tok-ci:user:ci-bot,ops:tok-ops:admin:ansible,node1:tok-n1:mounter:node-1
```

An identity is a short tag you attach to the token (`ci-bot`, `node-1`, `ansible`). The agent writes it into the `created-by` label on every resource the token creates, and checks that same label on every later change.

The rule is the same for every role that accepts an identity (`user`, `mounter`, `admin`): the identity lands in `created-by` on create, and every later change to that resource has to come from the same identity.

`readonly` does not accept an identity (it cannot create or modify anything).

A token without an identity keeps Level 2 behavior unchanged. That's useful for operational break-glass: an `admin` token without identity can still reach every resource, even ones owned by identified tokens.

### Extra guarantees Level 3 adds

**Source-ownership**. Without it, another identity in your tenant could snapshot your volume and clone that snapshot to walk away with a copy of your data. The agent blocks that by checking ownership against the **source** (the volume or snapshot being acted on), not just the new resource. Covers snapshots, clones, exports, and defragment tasks. See the matrix below for the full list.

**Update boundary**. When a client replaces a volume's labels, they cannot change `created-by` or set the `tenant` label. If they leave `created-by` out, the existing value stays. Without this guard, an owner could overwrite `created-by` and hand off the volume or its history. Admins follow the same rule: their ownership bypass doesn't let them rewrite `created-by`.

### Migration (pre v0.10.0)

Resources from before Level 3 don't carry a `created-by`. Without it, ownership checks pass through and any token with the right role can change them. Set `created-by` on existing resources to bring them under ownership.

## Per-endpoint matrix

### Role access

| Role       | Read | Create | Update | Delete | FS-global tasks |
|------------|:----:|:------:|:------:|:------:|:---------------:|
| `readonly` |  ✓   |        |        |        |                 |
| `mounter`  |  ✓   |  ✓¹    |        |  ✓¹    |                 |
| `user`     |  ✓   |   ✓    |   ✓²   |   ✓    |                 |
| `admin`    |  ✓   |   ✓    |   ✓²   |   ✓    |        ✓        |

¹ Mounter create/delete is limited to exports.

² Update applies only to volumes. Snapshots, clones, exports, and tasks have no update path.

FS-global tasks are `scrub`, `balance`, `quota-rescan`. They affect the whole filesystem, not a single tenant or volume.

### created-by per endpoint

Two terms used below:

- **`created-by`** is the persisted label on a resource (audit trail, lives in the metadata file).
- **`identity`** is the per-token tag configured in `AGENT_TENANTS` (the runtime caller).

The agent enforces `created-by == identity` on create, and `identity == created-by` on later changes. Same value, two sides of one rule.

- `Enforce created-by`: on create, the server writes the trusted `identity` into the resource's `created-by` label. A different client-supplied value is rejected with 403.
- `Owner`: the caller's `identity` must match the resource's `created-by`.
- `Owner*`: checked against the **source** resource's `created-by` (the volume or snapshot being acted on), not the new resource.

| Endpoint             | Enforce created-by | Owner |
|----------------------|:------------------:|:-----:|
| Create Volume        |   ✓   |       |
| Update Volume        |  ✓¹   |   ✓   |
| Delete Volume        |       |   ✓   |
| Clone / CreateClone  |   ✓   |   ✓*  |
| Create Snapshot      |   ✓   |   ✓*  |
| Delete Snapshot      |       |   ✓   |
| Create Export        |   ✓   |   ✓*  |
| Delete Export        |       |   ✓*  |
| Create Defragment    |   ✓   |   ✓*  |
| Create FS-global     |   ✓   |       |
| Cancel Task          |       |   ✓   |

¹ Update Volume doesn't write a new `created-by`. Instead it enforces three rules: clients cannot set the `tenant` label (the server tracks which tenant a resource belongs to itself), `created-by` cannot be changed, and replacing the labels keeps the existing `created-by` even if the client omits it.

## Denial telemetry

Every `403` is counted in the `http_requests_total{reason=...}` Prometheus counter and logged as a warning with client IP, tenant, path, token fingerprint, and identity (when set).

| `reason`            | Why                                                                       |
|---------------------|---------------------------------------------------------------------------|
| `invalid_token`     | The Bearer or Basic token didn't match any entry in `AGENT_TENANTS`. |
| `role_denied`       | The token's role is not allowed for this method, path, or task type. The role is in the warning log. |
| `identity_mismatch` | The client supplied a `created-by` that didn't match the token's identity (on create) or the existing value (on update). |
| `ownership`         | The token's identity does not match the resource's `created-by`. |

## Token fingerprints

Tokens are never returned by the API. Each token has a stable fingerprint, a 64-character hex string that maps to exactly one configured token without exposing the token itself, so you can correlate audit logs to a token without ever seeing the token in the clear.

- `GET /v1/whoami`: returns the caller's tenant, role, identity, and fingerprint.
- `GET /v1/tokens`: admin only. Lists the caller's own tenant and every token configured for it (role, identity, fingerprint). Cross-tenant visibility is not exposed.

CLI:

```
btrfs-nfs-csi whoami
btrfs-nfs-csi tokens
```

Table view truncates each fingerprint to the first 12 hex chars (Git-style short prefix). Use `-o wide` for the full 64-char hex when you need to grep audit logs by full value, or `-o json` for the raw API response.

Fingerprints are derived from a per-installation secret stored at `AGENT_BASE_PATH/metadata/root_secret`, which the agent generates on first boot. A `root_secret.bak` copy sits next to it as a safety net. If the two ever drift apart (disk corruption, partial restore, accidental edit), the agent refuses to start so it does not silently rotate every fingerprint in your audit log.

## Hashed tokens

The token field in `AGENT_TENANTS` accepts either a plaintext value or a bcrypt password hash. The agent detects hashes by their `$2a$` / `$2b$` / `$2y$` prefix. Anything else is treated as plaintext for back-compat.

```
AGENT_TENANTS=ops:$2y$12$KIXp.../iZ8eqwjN12iKy:admin,ci:plain-tok:user:ci-bot
```

Generate a hash via the bundled CLI:

```
btrfs-nfs-csi hash-token --cost 12
# Token: ********           (no echo, or pipe via stdin)
# $2a$12$KIXp.../iZ8eqwjN12iKy
```

Defaults to cost 12. The same binary that runs the agent generates the hash, so no extra tooling is required. `htpasswd -nbB` works too, but stock OpenSSL does not produce bcrypt.

The agent never logs or returns the raw token. The fingerprint shown in `/v1/whoami` and `/v1/tokens` is derived from whatever is in `AGENT_TENANTS`, plaintext or hash, and stays the same across restarts as long as the config and `root_secret` don't change.

Performance: the slow bcrypt check only runs the first time the agent sees a given token. After that the agent remembers it, so working clients don't keep paying the bcrypt cost on every request. Wrong tokens always pay the full check (there is no shortcut for unknown tokens, by design).

### Side note

Bcrypt is intentionally slow, which is what protects your tokens if someone gets a copy of `AGENT_TENANTS`. The trade-off is that the agent has to run that slow check for every rejected request, including ones from somebody who is just spamming the agent with random guesses. It runs the check once for each bcrypt entry, so a long list multiplies the cost of every wrong guess.

As a rough guide: with ten bcrypt entries at the default cost setting, a single bad request takes around two and a half seconds of CPU on a small server, and an attacker can fire off thousands of those without ever holding a real token. Valid tokens are not affected after their first use because the agent remembers them.

For now, the practical advice is to keep the bcrypt list short (a handful of entries at most), and stick with plain tokens for ones you generated with a random tool like `openssl rand -hex 24`, since those are already so long nobody can guess them anyway. A planned follow-up replaces bcrypt with a faster verification method that stays cheap regardless of list length, which removes this exposure entirely.

Bcrypt hashes contain `$`, so single-quote the value to keep the shell from eating the markers: `AGENT_TENANTS='ops:$2y$12$...:admin'`.

## Validation reference

| Field            | Allowed characters        | Length | Notes                                          |
|------------------|---------------------------|--------|------------------------------------------------|
| tenant name      | `a-zA-Z0-9_-`             | 1-128  | `tasks`, `snapshots`, `data`, `metadata` reserved |
| token            | plaintext or bcrypt hash  | 1+     | must not contain `,` or `:`. `$2a$/$2b$/$2y$` recognized as bcrypt (see [Hashed tokens](#hashed-tokens)) |
| role             | enum                      | fixed  | `readonly`, `mounter`, `user`, `admin`         |
| identity         | `a-zA-Z0-9_-`             | 1-32   | not accepted on `readonly`                     |

All fields are validated at startup. A bad value aborts the agent with a clear error.
