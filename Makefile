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
REMOTE        ?= ec2-user@api.pkg.wago.sh
REMOTE_DIR    ?= /opt/wago-registry
REMOTE_SVC    ?= wago-registry
DEPLOY_GOARCH ?= amd64

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

## ── deploy the backend to your server ────────────────────────────────────────

.PHONY: build-linux
build-linux: ## Cross-compile the backend for the server (linux/$(DEPLOY_GOARCH))
	cd backend && CGO_ENABLED=0 GOOS=linux GOARCH=$(DEPLOY_GOARCH) \
		go build -trimpath -ldflags "-s -w" -o registry ./cmd/registry
	@echo "▸ built backend/registry (linux/$(DEPLOY_GOARCH))"

.PHONY: redeploy
redeploy: ## Rebuild + restart the backend ON THIS machine (run this while on the server)
	cd backend && go build -trimpath -o registry ./cmd/registry
	sudo install -m 0755 backend/registry $(REMOTE_DIR)/registry
	sudo systemctl restart $(REMOTE_SVC)
	@sleep 1; systemctl is-active $(REMOTE_SVC) && curl -fsS localhost:$(API_PORT)/api/health \
		&& echo "  ✓ redeployed + healthy" || echo "  ⚠ check: sudo journalctl -u $(REMOTE_SVC) -n50"

.PHONY: redeploy-remote
redeploy-remote: build-linux ## From your dev machine: cross-compile, upload, and restart on $(REMOTE)
	scp backend/registry $(REMOTE):/tmp/wago-registry.new
	ssh $(REMOTE) 'set -e; \
		sudo install -m 0755 /tmp/wago-registry.new $(REMOTE_DIR)/registry; rm -f /tmp/wago-registry.new; \
		sudo systemctl restart $(REMOTE_SVC); sleep 1; \
		systemctl is-active $(REMOTE_SVC); curl -fsS localhost:$(API_PORT)/api/health; \
		echo "  ✓ redeployed + healthy on $(REMOTE)"'
