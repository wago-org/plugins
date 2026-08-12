# wago registry backend

HTTP backend for the wago plugins registry (**plugins.wago.sh**).

A **package** is a Go module with v1 metadata in `wago.json`, one explicit
register catalog, and a generated `wago.providers.json` snapshot committed before
the release is tagged. Each catalog entry yields an immutable **Plugin
Definition**, its canonical digest, and the exact provider import path. The
registry stores that reviewed metadata together with the source module version
and Go `h1:` checksum; it never infers providers from `init`, scans arbitrary
packages, or executes plugin code.

Plugin IDs and dependency IDs are full canonical Go package paths. Authority
requests are exact, non-hierarchical names with authority-specific scopes.
Compatibility is coarse engine/platform metadata; it is not a syscall matrix.
Configuration schemas must be closed JSON Schema objects so unknown settings fail
closed.

GitHub is the **only** identity provider — no passwords are ever stored. The
backend holds the GitHub OAuth client secret, verifies the identity, and issues
its own signed-cookie session. The full registry — packages, users, stars,
reviews, review votes, comments and install history — lives in the configured
Pebble or JSON store, seeded on first run from `data/packages.json` (schema
`wago-registry/v1`).

The service is pure Go and uses no cgo or SQLite. Pebble is the production
default; the compact JSON store remains available for development and auditing.

## Requirements

- Go 1.22+ for the build and the production runtime. Publish verification invokes
  `go mod download`; the `go` executable must be on the service's `PATH`.
- Outbound HTTPS access to `proxy.golang.org` and `sum.golang.org`. Published
  plugin modules must be publicly available through the Go proxy. Private
  modules and direct VCS fallback are intentionally disabled.

## Layout

```
backend/
  cmd/registry/main.go          # wiring: load config, open+seed store, build router, serve
  internal/config/config.go     # env -> Config
  internal/model/model.go       # persisted registry domain types
  internal/model/plugin.go      # strict v1 manifest, definitions, digests, checksums
  internal/model/semver.go      # Wago-compatible semantic-version range grammar
  internal/store/store.go       # Store interface
  internal/store/jsonstore.go   # JSON-file implementation (RWMutex, atomic write)
  internal/store/pebblestore.go # production embedded-LSM implementation
  internal/store/migrate.go     # one-time transactional v0-store cutover
  internal/store/seed.go        # import data/packages.json into an empty store
  internal/auth/session.go      # signed-cookie sessions
  internal/auth/github.go       # OAuth code exchange + user/email fetch
  internal/httpx/httpx.go       # writeJSON / writeError / CORS
  internal/api/plugins.go       # immutable plugin resolution + graph validation
  internal/api/source_verifier.go # exact module checksum + artifact catalog verification
  internal/api/*.go             # auth, packages, social, installs, publish, management
```

Import direction (no cycles): `api` → {`model`,`store`,`auth`,`httpx`,`config`};
`auth` → {`config`,`store`,`model`}; `store` → `model`; `config` → (stdlib only).

## Environment variables

| Variable               | Default                    | Description |
|------------------------|----------------------------|-------------|
| `PORT`                 | `8787`                     | Port the HTTP server listens on. |
| `GITHUB_CLIENT_ID`     | *(empty)*                  | GitHub OAuth app client id. |
| `GITHUB_CLIENT_SECRET` | *(empty)*                  | GitHub OAuth app client secret. |
| `OAUTH_REDIRECT_URL`   | *(empty)*                  | The backend's own callback URL, e.g. `http://localhost:8787/auth/github/callback`. Must match the GitHub app's callback URL exactly. |
| `FRONTEND_URL`         | `http://localhost:8000`    | Where to redirect the browser after login; also the allowed CORS origin. Prod: `https://plugins.wago.sh`. |
| `SESSION_SECRET`       | *(random, ephemeral)*      | HMAC key for signing session cookies. If empty, a random key is generated at startup (sessions won't survive a restart). **Required in production.** |
| `PACKAGES_FILE`        | `./data/packages.json`     | `wago-registry/v1` seed file. Imported into the store only when the store is empty. |
| `STORE_ENGINE`         | `pebble`                   | Persistent store engine: `pebble` or `json`. |
| `STORE_DIR`            | `./data/registry-db`       | Pebble database directory. |
| `STORE_FILE`           | `./data/store.json`        | JSON store path when `STORE_ENGINE=json`. |
| `DEV_MODE`             | `false`                    | When `true`, cookies are not marked `Secure` (so localhost http works). |
| `COOKIE_DOMAIN`        | *(empty)*                  | Optional cookie `Domain` for production (e.g. `.wago.sh`). Leave empty in dev. |

### Env files

Two environment-specific templates live here (committed); their real
counterparts (`dev.env`, `prod.env`) hold secrets and are git-ignored:

| Template | Copy to | Used by |
|----------|---------|---------|
| [`dev.env.example`](./dev.env.example)  | `dev.env`  | `make dev` / `make api` (local) |
| [`prod.env.example`](./prod.env.example) | `prod.env` | the server (systemd `EnvironmentFile`) |

`make env` creates both from the examples. `make api` loads `dev.env` by
default; run `make api ENV_FILE=prod.env` to test the prod config locally.

## Registering the GitHub OAuth app

Sign-in uses a **GitHub OAuth App** (not a GitHub App) — the flow only reads the
user's public profile and email. A classic OAuth App has a single callback URL,
so use **two apps**: one for dev, one for prod.

1. <https://github.com/settings/developers> → **OAuth Apps** → **New OAuth App**.
2. **Authorization callback URL** must equal `OAUTH_REDIRECT_URL` exactly:
   - dev app  → `http://localhost:8787/auth/github/callback`
   - prod app → `https://api.plugins.wago.sh/auth/github/callback`
3. Copy the **Client ID** and generate a **Client secret** into
   `GITHUB_CLIENT_ID` / `GITHUB_CLIENT_SECRET` in the matching env file.
4. The login flow requests the scopes `read:user user:email` (set in code; no app
   config needed).

For production, also set `SESSION_SECRET` (`openssl rand -hex 32`),
`DEV_MODE=false`, and `COOKIE_DOMAIN=.wago.sh` so the session cookie is shared
between `plugins.wago.sh` (site) and `api.plugins.wago.sh` (API).

## Running locally

From the repo root:

```sh
make env      # once: writes backend/dev.env + backend/prod.env
# edit backend/dev.env → GITHUB_CLIENT_ID / GITHUB_CLIENT_SECRET
make dev      # backend + tsc --watch + static site together
```

Or the backend alone: `make api` (loads `dev.env`). Then
`http://localhost:8787/auth/github/login` starts the OAuth flow.

Build / vet / format: `make build-api`, `make vet`, `make fmt` (or the raw
`go build ./...`, `go vet ./...`, `gofmt -l .`).

## First v1 store startup

The first v1 binary startup upgrades an unmarked registry store before serving
requests. Stop the old process and back up the configured data path first:

- Pebble: copy the complete `STORE_DIR` while the service is stopped.
- JSON: copy `STORE_FILE` while the service is stopped.

The upgrade accepts a legacy package only when its storage key, stored module,
and exact `https://github.com/<owner>/<repo>` repository all identify the same
GitHub repository. It then moves the package to its full
`github.com/<owner>/<repo>` key and rewrites stars, reviews, comments, reports,
notifications, and install history to that key. Safe package metadata and
ownership settings remain intact.

Legacy releases are not v1 releases: they lack the exact source checksum,
provider catalog, definition digest, and release fingerprint. The upgrade keeps
their original package JSON in private `quarantinedV0Packages` records, but
removes those releases, legacy dependency summaries, and old subpackage records
from the active package. They never appear in v1 candidate or resolution output.
The owner can immediately publish a new strict v1 release at the canonical ID.

The JSON rewrite uses an atomic rename. Pebble rewrites all affected records,
escaped package-scoped keys, quarantine records, and the schema marker in one
synced batch. Restarting the v1 binary is idempotent. An unprovable identity,
duplicate canonical identity, malformed key, or newer schema stops startup
without committing the migration; fix the data deliberately rather than
choosing a package implicitly.

Rolling back the binary alone is unsafe because the old binary does not
understand the v1 key layout or schema marker. Stop the service and restore the
complete pre-upgrade backup before starting the old binary. The quarantine is an
audit/recovery source, not an automatic downgrade mechanism.

## Applying changes

- **Backend env** (`.env`/secrets): restart the process so it re-reads the file.
  Local: Ctrl-C, `make api`. Prod: edit `prod.env`, `sudo systemctl restart wago-registry`.
- **Backend code:** rebuild and restart. Local: `make api` re-runs `go run`. Prod:
  `git pull && make build-api && sudo systemctl restart wago-registry` (or redeploy the binary).
- **Frontend code or `apiBase`:** rebuild and redeploy. Local: `make build` (or
  `make watch`) then refresh. Prod: push to `main` — the Pages workflow rebuilds
  `dist/` and publishes it.
- **Seed index (`data/packages.json`):** only imported into an *empty* store. To
  re-seed after editing it, clear the store first: `make reset-store` (⚠ drops
  user data), then restart. Existing packages are otherwise managed via `POST /api/publish`.

## Endpoints

Every package returned (list item or detail) is the stored record plus derived
fields: `version` / `latestVersion` (the latest version string), `updatedAt`
(RFC3339 of the latest version's publish time), `stars` (seed baseline + live
registry stars), `starred` (only when authenticated), `installsWeek`,
`installsWeekLabel` (compact: `4.2M` / `48.2k` / plain), and `installsTotal`.

| Method            | Path                              | Auth | Description |
|-------------------|-----------------------------------|------|-------------|
| `GET`             | `/api/health`                     | no   | `{"ok":true,"packages":N}` |
| `GET`             | `/api/auth/github/client`         | no   | GitHub OAuth client id and scopes for CLI device authorization. |
| `POST`            | `/api/auth/github/exchange`       | no   | Verify a GitHub access token and exchange it for a registry API token. |
| `GET`             | `/auth/github/login`              | no   | 302 → GitHub authorize (sets signed `wago_oauth_state`). |
| `GET`             | `/auth/github/callback`           | no   | Verify state, exchange code, upsert user, set session, 302 → frontend. |
| `POST`            | `/api/logout`                     | no   | Clear the session cookie. |
| `GET`             | `/api/me`                         | yes  | Current user, or 401. |
| `GET`             | `/api/packages`                   | no   | `{"packages":[...],"total":N}`. Query: `q`, `category`, `tag`, `stability`, `engine`, `verified=true`, `sort=popular\|quality\|recent`. `engine` matches packages whose `compatibility.engines` has that key. |
| `GET`             | `/api/packages/{name}`            | no   | Single package by its percent-encoded full canonical module ID. 404 if absent. Adds `starred` when authed. |
| `GET`             | `/api/packages/{name}/versions`   | no   | `{"versions":[...]}` newest first. |
| `GET`             | `/api/v1/plugins/resolve`         | no   | Highest compatible provider for `id`; repeat `range` for an intersection. Returns exact source, provider, digest-bearing definition, and release fingerprint. |
| `GET`             | `/api/v1/plugins/candidates`      | no   | All compatible providers, semantic-version newest first. Query: `id`, repeated `range`, `includeDeprecated=true`, `limit` (1–256), and `offset`. Response includes `total` and `nextOffset` when another page exists. |
| `POST`            | `/api/packages/{name}/installs`   | no   | Record one install for today; optional `{"version":".."}` body. → `{installsTotal,installsWeek,installsWeekLabel}`. |
| `GET`             | `/api/packages/{name}/installs`   | no   | `?days=90` → `{series:[{date,count}],total,week,weekLabel}`. |
| `POST` / `DELETE` | `/api/packages/{name}/star`       | yes  | Add / remove star → `{stars,starred}`. |
| `GET`             | `/api/packages/{name}/reviews`    | no   | `?sort=recent\|helpful` → `{reviews:[...],summary:{average,count}}`. Summary falls back to the package's seed rating when there are no reviews. |
| `POST`            | `/api/packages/{name}/reviews`    | yes  | Body `{rating:1-5, body}`; one review per user (upsert). |
| `POST`            | `/api/reviews/{id}/vote`          | yes  | Body `{dir:"up"\|"down"\|null}`; cannot vote on your own review. → `{score,upvotes,downvotes,myVote}`. |
| `GET`             | `/api/packages/{name}/comments`   | no   | `{comments:[...]}` chronological; thread client-side by `parentId`. |
| `POST`            | `/api/packages/{name}/comments`   | yes  | Body `{body, parentId?}` (1–4000 chars). |
| `DELETE`          | `/api/comments/{id}`              | yes  | Author or package owner only → `{ok:true}`. |
| `POST`            | `/api/publish`                    | yes  | Create/update a package from a manifest + release (see below). |
| `DELETE`          | `/api/packages/{name}`            | own  | Unpublish the whole package (owner only). |
| `DELETE`          | `/api/packages/{name}/versions/{version}` | own | Unpublish one version; removes the package if it was the last. |
| `POST`            | `/api/packages/{name}/deprecate`  | own  | Body `{message?, version?, undo?}`. Deprecate the package (sets `deprecatedMessage`), or a specific `version` (`deprecated:true`), or reverse with `undo:true`. |
| `GET`             | `/auth/cli/login`                 | no   | `?port=&state=` → GitHub OAuth, then 302 to `http://127.0.0.1:<port>/callback?token=…&state=…` (CLI loopback login). |
| `POST`            | `/api/tokens`                     | yes  | Mint an API token → `{token, id, label, createdAt}` (plaintext shown once). |
| `GET`             | `/api/tokens`                     | yes  | List the caller's tokens (hashes omitted). |
| `DELETE`          | `/api/tokens/{id}`                | yes  | Revoke one of the caller's tokens. |

**Auth column:** `yes` = a valid session cookie **or** `Authorization: Bearer <token>`;
`own` = authenticated **and** the package's owner. API tokens (minted at
`/api/tokens`, the CLI loopback login, or GitHub device authorization) let the
`wago` CLI and CI publish without a browser session. All interactive identities
are verified by GitHub; the backend does not run a separate device-identity
system.

> **Note on module names in API URLs.** `{name}` is one path parameter containing
> the full canonical module ID, so API clients percent-encode its slashes (for
> example `github.com%2Fwago-org%2Fwasi`). Public website detail URLs use the
> readable literal form `/github.com/wago-org/wasi` and accept no short alias.

## Publish flow

`POST /api/publish` (authenticated):

The tagged module must contain this generated snapshot at its root. Its
`providers` array has exactly the same entry shape as the publish request:

```json
{
  "$schema": "https://wago.sh/v1/providers.schema.json",
  "providers": [
    {
      "importPath": "github.com/acme/wago-pool/register",
      "definition": { "id": "github.com/acme/wago-pool", "version": "1.2.0" },
      "definitionDigest": "sha256:..."
    }
  ]
}
```

```json
{
  "manifest": {
    "$schema": "https://wago.sh/v1/schema.json",
    "package": {
      "module": "github.com/acme/wago-pool",
      "version": "1.2.0",
      "name": "Wago Pool",
      "description": "A bounded worker pool for Wago runtimes.",
      "license": "Apache-2.0",
      "repository": "https://github.com/acme/wago-pool",
      "authors": [{ "name": "Example Maintainer", "github": "example" }]
    }
  },
  "version": "1.2.0",
  "checksum": "h1:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
  "providers": [
    {
      "importPath": "github.com/acme/wago-pool/register",
      "definition": {
        "id": "github.com/acme/wago-pool",
        "name": "Pool",
        "version": "1.2.0",
        "provenance": {
          "repository": "https://github.com/acme/wago-pool",
          "license": "Apache-2.0"
        },
        "requires": [
          { "id": "github.com/wago-org/workers", "version": "^1.0.0" }
        ],
        "authorities": [
          {
            "name": "instance.manage",
            "mode": "required",
            "reason": "Own a bounded worker pool",
            "scope": { "maxInstances": 8, "maxMemoryBytes": 67108864 }
          }
        ]
      },
      "definitionDigest": "sha256:0000000000000000000000000000000000000000000000000000000000000000"
    }
  ],
  "commit": "abc123def",
  "notes": "Initial release.",
  "unpackedKB": 184
}
```

Behavior:

- The request, v1 manifest, nested settings, package metadata, provider catalog,
  definitions, authority scopes, config schemas, checksums, and digests are all
  decoded and validated strictly. Unknown fields are rejected.
- Provider catalogs come from the package's explicit register catalog;
  `wago.json` contains package metadata, not a duplicate provider list. Before a
  tag is published, tooling writes the canonical snapshot to
  `wago.providers.json` and checks it against `register.Providers`.
- After authorization and before any store write, the registry downloads the
  exact canonical `v`-prefixed module version through the public Go proxy,
  verifies its `h1:` sum, and strictly reads both `wago.json` and
  `wago.providers.json` (`$schema` =
  `https://wago.sh/v1/providers.schema.json`) from that artifact. It compares the
  complete `manifest.package` metadata and complete canonical definitions,
  digests, IDs, and import paths with the request. Project-level `plugins` and
  `settings` remain outside the publication identity comparison. The registry
  never imports or runs the plugin.
- The catalog must contain exactly one provider for `package.module` and each
  declared `package.subpackages` entry, with no undeclared or omitted IDs; every
  entry comes from the exact `<module>/register` import path.
- Each definition's version, display metadata, compatibility, provenance, and
  canonical SHA-256 digest must match the manifest and release.
- A Plugin ID is owned by one source module; a different module cannot later
  publish a provider under an existing ID.
- The complete dependency graph must resolve to one version per plugin without
  cycles or diamond conflicts. Required contracts must have a candidate in that
  graph; the consumer lockfile's explicit, ordered provider IDs make the actual
  required/optional/many binding.
- On first publish the verified GitHub author becomes `ownerLogin`. Later publishes
  require repository authority or an explicit publisher grant.
- The caller's login is added to `contributors` (deduped).
- Every version, including `0.0.0`, is immutable; publishing the same version again
  is **409**. `latest` is the greatest semantic version, not simply the last upload.
- A release fingerprint covers its exact source checksum and complete provider
  definitions. Publisher timestamps and popularity counters are excluded.

Resolution responses use this shape for each plugin:

```json
{
  "id": "github.com/acme/wago-pool",
  "version": "v1.2.0",
  "source": {
    "module": "github.com/acme/wago-pool",
    "version": "v1.2.0",
    "checksum": "h1:..."
  },
  "provider": { "importPath": "github.com/acme/wago-pool/register" },
  "definition": { "id": "github.com/acme/wago-pool", "version": "1.2.0" },
  "definitionDigest": "sha256:...",
  "releaseFingerprint": "sha256:..."
}
```

Requested authorities, configuration schema, compatibility, provenance,
dependencies, and provided/consumed Contracts are fields of `definition`; the
API does not emit redundant top-level copies that could drift from its digest.

`GET /api/v1/plugins/resolve` is a convenience lookup. Lockfile solvers should
use `/candidates`, follow `nextOffset` until absent, and solve the complete graph
before choosing a version.

## Session cookie

Cookie `wago_session` = `base64url(payload) + "." + base64url(HMAC-SHA256(payload, SESSION_SECRET))`
where `payload` is JSON `{uid, exp}` (exp is unix seconds, ~30-day expiry). Flags:
`HttpOnly`, `SameSite=Lax`, `Path=/`, `Secure` unless `DEV_MODE`. The HMAC is
verified in constant time and the expiry is checked on every authenticated request.

## Store format

With `STORE_ENGINE=json`, `STORE_FILE` is one document persisted atomically (temp
file + rename) on every mutation:

```json
{
  "storeSchemaVersion": 1,
  "users":    { "<userId>":   { "id":"..","login":"..","name":"..","avatarUrl":"..","email":".." } },
  "packages": { "<canonicalModuleID>": { ...Package... } },
  "stars":    { "<canonicalModuleID>": ["<userId>", "..."] },
  "reviews":  { "<reviewId>": { "id":"..","packageShort":"..","userId":"..","rating":5,"body":"..","createdAt":".." } },
  "votes":    { "<reviewId>": { "<userId>": "up" } },
  "comments": { "<commentId>":{ "id":"..","packageShort":"..","userId":"..","body":"..","createdAt":"..","parentId":".." } },
  "installs": { "<canonicalModuleID>": { "2026-07-06": 6885 } }
}
```

Pebble stores the same logical document one record per key. Package-scoped star
and install key segments are base64url encoded because canonical module IDs
contain slashes.

Seed users get stable ids of the form `seed:<login>`. Derived numbers (live star
totals, review `score = upvotes − downvotes`, install week/total) are recomputed at
read time.

## TinyGo / wago-on-WASI caveats

The code sticks to the standard library, but a few pieces are the friction points
for a TinyGo build targeting WASI:

- **`net/http` server.** Serving needs host socket imports; a WASI build would
  adapt the `http.Handler` to the runtime's request bridge.
- **Outbound TLS (`net/http` client + `crypto/tls`).** The GitHub OAuth calls use
  HTTPS, which TinyGo's WASI target does not fully support — route them through a
  host-provided fetch import.
- **Filesystem** (`os.ReadFile` / `os.WriteFile` / `os.Rename` for the store) needs
  WASI preopens; atomic-rename semantics depend on the host FS.
- **`crypto/rand`** needs a host entropy source (WASI `random_get`) — usually fine.
- **`encoding/json`, `crypto/hmac`, `crypto/sha256`, `encoding/base64`, `sync`,
  `time`, `sort`** are pure-Go and expected to port cleanly.
