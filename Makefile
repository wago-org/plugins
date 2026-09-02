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

# ── backend deploy (EC2) ─────────────────────────────────────────────────────
# Override for your server — inline (make redeploy-remote REMOTE=user@1.2.3.4) or
# by exporting in your shell. REMOTE is an ssh alias (~/.ssh/config) or user@host.
# REMOTE_DIR is the service WorkingDirectory, REMOTE_SVC the systemd unit name,
# and DEPLOY_GOARCH the server CPU (use arm64 for Graviton). Keep values comment-free.
REMOTE        ?= ec2-user@api.plugins.wago.sh
REMOTE_DIR    ?= /home/ec2-user/Wago/registry/backend
REMOTE_SVC    ?= wago-registry
DEPLOY_GOARCH ?= amd64
REMOTE_STORE_DIR ?= data/registry-db
REMOTE_API_PORT  ?= 8787
LOCAL_STORE_DIR  ?= backend/data/local-prod-db

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
	@echo "  make deploy-local   # production build + isolated snapshot of the production DB"

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
		STORE_ENGINE=$${STORE_ENGINE_OVERRIDE:-$${STORE_ENGINE:-pebble}} \
		STORE_DIR=$${STORE_DIR_OVERRIDE:-$${STORE_DIR:-data/registry-db}} \
		STORE_FILE=$${STORE_FILE:-data/store.json} \
		SESSION_SECRET=$${SESSION_SECRET:-dev-only-secret} \
		go run ./cmd/registry

.PHONY: pull-prod-db
pull-prod-db: ## Replace the isolated local DB with a credential-scrubbed production snapshot
	@REMOTE='$(REMOTE)' \
		REMOTE_DIR='$(REMOTE_DIR)' \
		REMOTE_SVC='$(REMOTE_SVC)' \
		REMOTE_STORE_DIR='$(REMOTE_STORE_DIR)' \
		LOCAL_STORE_DIR='$(abspath $(LOCAL_STORE_DIR))' \
		REMOTE_API_PORT='$(REMOTE_API_PORT)' \
		CONFIRM_PROD_DB_COPY='$(CONFIRM_PROD_DB_COPY)' \
		./scripts/pull-prod-db.sh

.PHONY: deploy-local
deploy-local: build-ts pull-prod-db ## Serve the production frontend + backend using an isolated production DB snapshot
	@echo "▸ local production snapshot: site http://localhost:$(WEB_PORT) · api :$(API_PORT)"
	@echo "  database: $(abspath $(LOCAL_STORE_DIR)) (OAuth/API credentials scrubbed)"
	@trap 'kill 0' INT TERM EXIT; \
		STORE_ENGINE_OVERRIDE=pebble STORE_DIR_OVERRIDE='$(abspath $(LOCAL_STORE_DIR))' \
			$(MAKE) --no-print-directory api & \
		python3 -m http.server $(WEB_PORT) --directory dist & \
		wait

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
check: typecheck test-routes test-lan test-auth-return test-install-stats test-wat test-api vet ## Run frontend contract checks and backend tests/vet

.PHONY: typecheck
typecheck: ## tsc --noEmit
	npm run typecheck

.PHONY: test-wat
test-wat: ## Compile and verify WebAssembly Text syntax highlighting
	npm run test:wat

.PHONY: test-routes
test-routes: ## Verify canonical v1 plugin URLs and reject aliases
	npm run test:routes

.PHONY: test-lan
test-lan: ## Check LAN dev API routing for phone and tablet testing
	npm run test:lan

.PHONY: test-auth-return
test-auth-return: ## Verify GitHub login returns to the previous non-auth page
	npm run test:auth-return

.PHONY: test-install-stats
test-install-stats: ## Verify monthly install totals survive API fallback rendering
	npm run test:install-stats

.PHONY: test-api
test-api: ## Run backend unit and contract tests
	cd backend && go test ./...

.PHONY: vet
vet: ## go vet + gofmt check
	cd backend && go vet ./...
	@cd backend && files="$$(gofmt -l .)"; test -z "$$files" || (echo "gofmt needed:"; echo "$$files"; exit 1)

.PHONY: fmt
fmt: ## gofmt -w the backend
	cd backend && gofmt -w .

## ── housekeeping ─────────────────────────────────────────────────────────────

.PHONY: clean
clean: ## Remove build artifacts (dist, compiled JS, backend binary+store)
	rm -rf dist assets/js backend/registry backend/registry-backend backend/data/store.json backend/data/registry-db

.PHONY: reset-store
reset-store: ## Delete the backend's data store (re-seeds from data/packages.json on next run)
	rm -f backend/data/store.json backend/data/store.json.tmp
	rm -rf backend/data/registry-db
	@echo "store cleared — it will re-seed on the next `make api`"

## ── deploy the backend to your server ────────────────────────────────────────

.PHONY: build-linux
build-linux: ## Cross-compile the backend for the server (linux/$(DEPLOY_GOARCH))
	cd backend && CGO_ENABLED=0 GOOS=linux GOARCH=$(DEPLOY_GOARCH) \
		go build -trimpath -ldflags "-s -w" -o registry ./cmd/registry
	@echo "▸ built backend/registry (linux/$(DEPLOY_GOARCH))"

.PHONY: redeploy
redeploy: ## Rebuild + restart the backend ON THIS machine (run this on the server, from the repo)
	cd backend && go build -trimpath -o registry.new ./cmd/registry && mv -f registry.new registry
	sudo systemctl restart $(REMOTE_SVC)
	@sleep 1; systemctl is-active $(REMOTE_SVC) && curl -fsS localhost:$(API_PORT)/api/health \
		&& echo "  ✓ redeployed + healthy" || echo "  ⚠ check: sudo journalctl -u $(REMOTE_SVC) -n50"

.PHONY: redeploy-remote
redeploy-remote: build-linux ## From your dev machine: cross-compile, upload the binary, restart on $(REMOTE)
	scp backend/registry $(REMOTE):$(REMOTE_DIR)/registry.new
	ssh $(REMOTE) 'set -e; cd $(REMOTE_DIR); \
		mv -f registry.new registry; \
		sudo systemctl restart $(REMOTE_SVC); sleep 1; \
		systemctl is-active $(REMOTE_SVC); curl -fsS localhost:$(API_PORT)/api/health; \
		echo "  ✓ redeployed + healthy on $(REMOTE)"'
