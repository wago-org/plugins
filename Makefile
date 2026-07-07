# wago plugin registry — dev & build tasks.
#
# Two moving parts: a static TS frontend (served at :8000) and a Go backend
# (:8787). You can run the site with NO backend at all (sign-in / stars /
# reviews are faked in the browser) — that's `make web`. Run `make dev` to
# bring up the backend too and get real, shared data.

WEB_PORT   ?= 8000
API_PORT   ?= 8787
FRONTEND_URL ?= http://localhost:$(WEB_PORT)

# Which env file the backend loads (relative to backend/). Override for prod:
#   make api ENV_FILE=prod.env
ENV_FILE   ?= dev.env

# npx runs the locally-installed tsc; python serves the static files.
TSC   := npx tsc
SERVE := python3 -m http.server $(WEB_PORT)

.DEFAULT_GOAL := help

## ── help ─────────────────────────────────────────────────────────────────────

.PHONY: help
help: ## Show this help
	@echo "wago registry — make targets:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) \
		| sort \
		| awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'
	@echo ""
	@echo "Quick start:"
	@echo "  make install        # one-time: deps"
	@echo "  make web            # site only, at http://localhost:$(WEB_PORT) (no backend)"
	@echo "  make dev            # site + backend together (real data)"

## ── setup ────────────────────────────────────────────────────────────────────

.PHONY: install
install: ## Install deps (npm + go modules)
	npm install
	cd backend && go mod download

.PHONY: env
env: ## Create backend/{dev,prod}.env from the examples (edit them for GitHub login)
	@for e in dev prod; do \
		if [ -f backend/$$e.env ]; then \
			echo "backend/$$e.env exists — leaving it alone"; \
		else \
			cp backend/$$e.env.example backend/$$e.env; \
			echo "wrote backend/$$e.env"; \
		fi; \
	done
	@echo "→ fill in GITHUB_CLIENT_ID / GITHUB_CLIENT_SECRET (and a SESSION_SECRET for prod)"

## ── run ──────────────────────────────────────────────────────────────────────

.PHONY: dev
dev: ## Run backend + tsc --watch + static server together (Ctrl-C stops all)
	@echo "▸ backend :$(API_PORT)  ·  tsc --watch  ·  site http://localhost:$(WEB_PORT)"
	@trap 'kill 0' INT TERM EXIT; \
		$(MAKE) --no-print-directory api & \
		$(TSC) --watch --preserveWatchOutput & \
		$(SERVE) & \
		wait

.PHONY: web
web: build-ts ## Build the frontend once and serve it (no backend → local demo mode)
	@echo "▸ serving http://localhost:$(WEB_PORT)  (Ctrl-C to stop)"
	@$(SERVE)

.PHONY: watch
watch: ## Recompile TS on save (run alongside `make web` in another terminal)
	$(TSC) --watch --preserveWatchOutput

.PHONY: api
api: ## Run the Go backend at :$(API_PORT) (loads backend/$(ENV_FILE))
	@mkdir -p backend/data
	@cd backend && \
		if [ -f $(ENV_FILE) ]; then set -a; . ./$(ENV_FILE); set +a; \
		else echo "note: backend/$(ENV_FILE) not found — using defaults (run 'make env')"; fi; \
		DEV_MODE=$${DEV_MODE:-true} \
		PORT=$${PORT:-$(API_PORT)} \
		FRONTEND_URL=$${FRONTEND_URL:-$(FRONTEND_URL)} \
		OAUTH_REDIRECT_URL=$${OAUTH_REDIRECT_URL:-http://localhost:$(API_PORT)/auth/github/callback} \
		PACKAGES_FILE=$${PACKAGES_FILE:-../data/packages.json} \
		STORE_FILE=$${STORE_FILE:-data/store.json} \
		SESSION_SECRET=$${SESSION_SECRET:-dev-only-secret} \
		go run ./cmd/registry

## ── build ────────────────────────────────────────────────────────────────────

.PHONY: build
build: build-ts build-api ## Production build: dist/ + backend binary

.PHONY: build-ts
build-ts: ## Compile TS → assets/js and assemble dist/
	npm run build

.PHONY: build-api
build-api: ## Compile the backend to backend/registry
	cd backend && go build -o registry ./cmd/registry

## ── checks ───────────────────────────────────────────────────────────────────

.PHONY: check
check: typecheck vet ## Typecheck the frontend and vet the backend

.PHONY: typecheck
typecheck: ## tsc --noEmit
	npm run typecheck

.PHONY: vet
vet: ## go vet + gofmt check
	cd backend && go vet ./... && test -z "$$(gofmt -l .)" || (echo "gofmt needed:"; gofmt -l backend; exit 1)

.PHONY: fmt
fmt: ## gofmt -w the backend
	cd backend && gofmt -w .

## ── housekeeping ─────────────────────────────────────────────────────────────

.PHONY: clean
clean: ## Remove build artifacts (dist, compiled JS, backend binary+store)
	rm -rf dist assets/js backend/registry backend/registry-backend backend/data/store.json

.PHONY: reset-store
reset-store: ## Delete the backend's data store (re-seeds from data/packages.json on next run)
	rm -f backend/data/store.json backend/data/store.json.tmp
	@echo "store cleared — it will re-seed on the next `make api`"
