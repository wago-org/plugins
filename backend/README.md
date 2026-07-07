# wago registry backend

HTTP backend for the wago plugins registry (**pkg.wago.sh**).

A **package** is a Go module that ships a `wago-plugin.json` manifest exposing one
or more **extensions** (host imports, codegen backends, WASI surfaces, …).
Compatibility is the manifest's `compatibility` block — semver ranges keyed by
engine (`wago` / `tinygo` / `go`) plus supported `platforms` — together with a
`stability` (`experimental` | `stable` | `deprecated`) and coarse `capabilities`.
It is **not** a per-syscall matrix.

GitHub is the **only** identity provider — no passwords are ever stored. The
backend holds the GitHub OAuth client secret, verifies the identity, and issues
its own signed-cookie session. The full registry — packages, users, stars,
reviews, review votes, comments and install history — lives in a single JSON-file
store, seeded on first run from `data/packages.json` (schema `wago-registry/v1`).

The code is **standard-library only** (no third-party modules, no cgo, no SQLite)
so it can, in principle, be compiled with TinyGo to run on the `wago` WASM runtime
later.

## Requirements

- Go 1.22+ (uses `net/http` method+pattern routing).

## Layout

```
backend/
  cmd/registry/main.go          # wiring: load config, open+seed store, build router, serve
  internal/config/config.go     # env -> Config
  internal/model/model.go       # domain types + the wago-plugin/v1 Manifest
  internal/store/store.go       # Store interface
  internal/store/jsonstore.go   # JSON-file implementation (RWMutex, atomic write)
  internal/store/seed.go        # import data/packages.json into an empty store
  internal/auth/session.go      # signed-cookie sessions
  internal/auth/github.go       # OAuth code exchange + user/email fetch
  internal/httpx/httpx.go       # writeJSON / writeError / CORS
  internal/api/*.go             # App + handlers (auth, packages, social, installs, publish)
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
| `FRONTEND_URL`         | `http://localhost:8000`    | Where to redirect the browser after login; also the allowed CORS origin. Prod: `https://pkg.wago.sh`. |
| `SESSION_SECRET`       | *(random, ephemeral)*      | HMAC key for signing session cookies. If empty, a random key is generated at startup (sessions won't survive a restart). **Required in production.** |
| `PACKAGES_FILE`        | `./data/packages.json`     | `wago-registry/v1` seed file. Imported into the store only when the store is empty. |
| `STORE_FILE`           | `./data/store.json`        | Read/write JSON store for the whole registry. Created if missing. |
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
   - prod app → `https://api.pkg.wago.sh/auth/github/callback`
3. Copy the **Client ID** and generate a **Client secret** into
   `GITHUB_CLIENT_ID` / `GITHUB_CLIENT_SECRET` in the matching env file.
4. The login flow requests the scopes `read:user user:email` (set in code; no app
   config needed).

For production, also set `SESSION_SECRET` (`openssl rand -hex 32`),
`DEV_MODE=false`, and `COOKIE_DOMAIN=.wago.sh` so the session cookie is shared
between `pkg.wago.sh` (site) and `api.pkg.wago.sh` (API).

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
| `GET`             | `/auth/github/login`              | no   | 302 → GitHub authorize (sets signed `wago_oauth_state`). |
| `GET`             | `/auth/github/callback`           | no   | Verify state, exchange code, upsert user, set session, 302 → frontend. |
| `POST`            | `/api/logout`                     | no   | Clear the session cookie. |
| `GET`             | `/api/me`                         | yes  | Current user, or 401. |
| `GET`             | `/api/packages`                   | no   | `{"packages":[...],"total":N}`. Query: `q`, `category`, `tag`, `stability`, `engine`, `verified=true`, `sort=popular\|quality\|recent`. `engine` matches packages whose `compatibility.engines` has that key. |
| `GET`             | `/api/packages/{name}`            | no   | Single package (match `short`, or `name` when it has no slashes). 404 if absent. Adds `starred` when authed. |
| `GET`             | `/api/packages/{name}/versions`   | no   | `{"versions":[...]}` newest first. |
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
`/api/tokens` or via the CLI loopback login) let the `wago` CLI and CI publish
without a browser session.

> **Note on module names in the URL.** `{name}` matches a single path segment, so
> a full module path with slashes cannot appear in the URL — the frontend uses the
> package's `short` id. `GetPackage` still resolves a `name` value that has no
> slashes.

## Publish flow

`POST /api/publish` (authenticated):

```json
{
  "manifest": { "schema": "wago-plugin/v1", "module": "github.com/acme/wago-redis",
                "extensions": [ { "import": "...", "id": "...", "stability": "stable",
                                  "tags": ["..."], "compatibility": { "engines": {...} } } ] },
  "version": "v1.2.0", "commit": "abc123def", "notes": "...",
  "unpackedKB": 184, "category": "host-imports", "tags": []
}
```

Behavior:

- `short` is derived from the module path's last element with a leading `wago-` /
  `wago_` stripped (`github.com/acme/wago-redis` → `redis`).
- On **first** publish the caller becomes `ownerLogin`; later publishes require the
  caller to be the owner (**403** otherwise).
- Package metadata is aggregated from the manifest's extensions: union of tags,
  and the first non-empty description/stability; the first extension's
  `compatibility` becomes the package-level one, while each extension keeps its own.
- The caller's login is added to `contributors` (deduped).
- The version is appended, marked `latest` (unsetting the previous latest), and
  `updatedAt` is bumped. A **duplicate** version string is **409**.
- **400** if `manifest.schema != "wago-plugin/v1"`, or `module` / `version` is empty.

## Session cookie

Cookie `wago_session` = `base64url(payload) + "." + base64url(HMAC-SHA256(payload, SESSION_SECRET))`
where `payload` is JSON `{uid, exp}` (exp is unix seconds, ~30-day expiry). Flags:
`HttpOnly`, `SameSite=Lax`, `Path=/`, `Secure` unless `DEV_MODE`. The HMAC is
verified in constant time and the expiry is checked on every authenticated request.

## Store format

`STORE_FILE` is a single JSON document, persisted atomically (temp file + rename)
on every mutation:

```json
{
  "users":    { "<userId>":   { "id":"..","login":"..","name":"..","avatarUrl":"..","email":".." } },
  "packages": { "<short>":    { ...Package... } },
  "stars":    { "<short>":    ["<userId>", "..."] },
  "reviews":  { "<reviewId>": { "id":"..","packageShort":"..","userId":"..","rating":5,"body":"..","createdAt":".." } },
  "votes":    { "<reviewId>": { "<userId>": "up" } },
  "comments": { "<commentId>":{ "id":"..","packageShort":"..","userId":"..","body":"..","createdAt":"..","parentId":".." } },
  "installs": { "<short>":    { "2026-07-06": 6885 } }
}
```

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
