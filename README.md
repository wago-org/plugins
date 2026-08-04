# plugins.wago.sh — the wago plugin registry

Browse and publish plugins for [**wago**](https://github.com/wago-org/wago):
A wonderfully quick, compact, and extensible WebAssembly runtime for Go.
Host-import bundles, WASI shims, debuggers and codegen backends are drop-in Go
modules for the runtime.

Two pieces:

- **Frontend** — a static single-page app (plain HTML + CSS + TypeScript, no
  framework, compiled by `tsc`). Hosted on **GitHub Pages** at `plugins.wago.sh`.
- **Backend** — a small **Go** service ([`backend/`](backend/)) that does GitHub
  OAuth, sessions, and stores stars / reviews / votes. Standard-library only
  (with an eye toward a future TinyGo → wago build).

The site is designed to **degrade gracefully**: with no backend reachable it
still runs entirely from the static package index, faking sign-in, stars and
reviews in the browser so the whole UI is explorable. Point it at a live backend
and those become real and shared.

The production build also prerenders complete catalog, package, and author
content for clients that do not execute JavaScript. It publishes `/llms.txt`,
`/llms-full.txt`, `/data/catalog.json`, Schema.org JSON-LD, `robots.txt`, and a
package-aware sitemap from the same live API response used for the page files.

## Identity: GitHub only

There are no passwords anywhere. Sign-in is GitHub OAuth; the backend holds the
client secret, verifies the GitHub identity, and issues its own signed-cookie
session. We store only the GitHub id, login, name, avatar and email.

## Layout

```
index.html            # SPA shell — mounts #app, loads the module bundle
data/
  packages.json       # the package index (drives both frontend and backend)
src/                  # TypeScript source
  head.ts             #   external runtime configuration loaded before the SPA
  main.ts             #   boot
  app.ts              #   render loop, hash router, event delegation
  screens.ts          #   pure screen render functions (home/search/package/auth/account)
  api.ts              #   data layer: backend client + local (no-backend) fallback
  state.ts            #   the single app-state object
  types.ts util.ts    #   shapes + helpers
  config.ts           #   where the backend lives (window.WAGO_CONFIG override)
  copy.ts             #   copy-to-clipboard buttons
assets/css/tokens.css # base + palette (dark-only "sparkle" theme)
assets/css/crawler.css # build-time crawler/no-JS catalog styling
assets/js/            # compiled output (git-ignored)
backend/              # the Go service — see backend/README.md
CNAME                 # plugins.wago.sh
.github/workflows/
  deploy.yml          # build + publish to GitHub Pages
```

## Develop

Everything is driven by the [`Makefile`](Makefile) — run `make` (or `make help`)
to see all targets.

```bash
make install     # one-time: npm + go deps
make web         # site only, at http://localhost:8000 — no backend needed
make dev         # site + backend together (real, shared data)
```

`make web` builds the TypeScript once and serves the static site. With no
backend running, the frontend drops into **local-demo mode** automatically:
package data comes from the static index and sign-in / stars / reviews are faked
in the browser, so the whole UI is explorable.

`make dev` additionally runs the Go backend at `:8787` and `tsc --watch`, so
stars, reviews, comments, install history and publishing become real. Ctrl-C
stops all three.

Other useful targets:

```bash
make watch       # just tsc --watch (pair with `make web` in another terminal)
make api         # just the backend (reads backend/.env if present)
make check       # typecheck the frontend + vet the backend
make build       # production build: dist/ + backend binary
make reset-store # wipe the backend store; it re-seeds from data/packages.json
```

### Real GitHub sign-in (optional)

Local-demo mode fakes sign-in. For the real OAuth flow, register a GitHub OAuth
app (callback `http://localhost:8787/auth/github/callback`), then:

```bash
make env         # writes backend/.env from the example
# edit backend/.env → set GITHUB_CLIENT_ID and GITHUB_CLIENT_SECRET
make dev
```

See [`backend/README.md`](backend/README.md) for every env var.

## The package index

Everything on the site is driven by [`data/packages.json`](data/packages.json):
the package list, per-package readme/compat/versions/issues, stats and
categories. The backend reads the same file (read-only) and merges in live star
counts and user reviews from its own store. Update the index by committing to
this file (a CI job can regenerate it from the org's repos later).

## Deploy

- **Frontend:** pushing to `main` runs `.github/workflows/deploy.yml` — `npm ci`,
  `npm run build`, publish `dist/` to GitHub Pages. `CNAME` points it at
  `plugins.wago.sh`; set the matching custom domain in the repo's Pages settings and
  the DNS record at the registrar.
- **Backend:** deploy `backend/` anywhere that runs a Go binary (a small VM, Fly,
  Render, Cloud Run…). Set its `FRONTEND_URL` to `https://plugins.wago.sh` and, in
  the frontend, set `window.WAGO_CONFIG.apiBase` in `index.html` to the backend's
  URL (default guess is `https://api.plugins.wago.sh`). Register the GitHub OAuth
  app's callback as `<backend>/auth/github/callback`.
