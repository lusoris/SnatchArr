# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# SnatchArr monorepo verification entrypoint. `make verify-all` is the universal gate praetor
# audits; every component gate below is also runnable on its own.

SHELL := /bin/sh
.DEFAULT_GOAL := help

PRAETORCTL ?= praetorctl
# golusoris' shared Makefile is consumed from the module cache (docs/ci-downstream.md); the
# variables are recursively expanded so they resolve only when an api-* target runs.
GOMODCACHE       = $(subst \,/,$(shell cd api && go env GOMODCACHE))
GOLUSORIS_VER    = $(shell cd api && go list -m -f '{{.Version}}' github.com/golusoris/golusoris)
GOLUSORIS_SHARED = $(GOMODCACHE)/github.com/golusoris/golusoris@$(GOLUSORIS_VER)/tools/Makefile.shared

.PHONY: help verify-all api-verify worker-verify web-verify web-e2e proto-verify deploy-verify governance-verify \
        api-gen web-gen proto-gen dev hooks setup

help: ## Show this help
	@grep -hE '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-18s %s\n", $$1, $$2}'

verify-all: governance-verify proto-verify api-verify worker-verify web-verify deploy-verify ## Every gate (praetor universal verification)
	@echo "All SnatchArr verification gates passed."

# ── api/ (Go, golusoris) ──────────────────────────────────────────────────────
api-verify: ## golangci-lint + gosec + govulncheck + go test -race + generated-code drift + spectral
	@test -f api/go.mod || { echo "api/ not scaffolded yet; skipping"; exit 0; }
	$(MAKE) -C api -f "$(GOLUSORIS_SHARED)" ci GOLANGCI_CONFIG=../.golangci.yml
	cd api && gosec -quiet -conf ../.gosec.json -exclude-generated ./...
	cd api && go generate ./... && git diff --exit-code -- internal/oas internal/gen
	@if [ -f api/openapi/openapi.yaml ]; then npx --yes @stoplight/spectral-cli@6.15.0 lint api/openapi/openapi.yaml --ruleset tools/spectral.yaml --fail-severity warn; fi

api-gen: ## ogen + sqlc generation inside api/
	cd api && go generate ./...

# ── worker/ (Rust) ────────────────────────────────────────────────────────────
worker-verify: ## cargo fmt/clippy(-D warnings)/audit/deny/test
	@test -f worker/Cargo.toml || { echo "worker/ not scaffolded yet; skipping"; exit 0; }
	cd worker && cargo fmt --all --check
	cd worker && cargo clippy --workspace --all-targets --locked -- -D warnings
	cd worker && cargo audit
	cd worker && cargo deny check
	cd worker && cargo test --workspace --locked

# ── web/ (SvelteKit + sveltesentio) ───────────────────────────────────────────
web-verify: ## pnpm lint/typecheck/test(+coverage)/build + OpenAPI type drift
	@test -f web/package.json || { echo "web/ not scaffolded yet; skipping"; exit 0; }
	cd web && pnpm install --frozen-lockfile
	cd web && pnpm gen:api && git diff --exit-code -- src/lib/api/schema.d.ts
	cd web && pnpm lint && pnpm typecheck && pnpm test -- --coverage && pnpm build

web-gen: ## OpenAPI -> src/lib/api/schema.d.ts
	cd web && pnpm gen:api

web-e2e: ## Playwright + axe-core (zero violations; never suppressed)
	cd web && pnpm test:e2e

# ── proto/ ────────────────────────────────────────────────────────────────────
proto-verify: ## buf lint + breaking (vs main) + generation drift
	@test -f proto/buf.yaml || { echo "proto/ not scaffolded yet; skipping"; exit 0; }
	cd proto && buf lint
	@if git rev-parse --verify -q origin/main >/dev/null 2>&1; then cd proto && buf breaking --against '../.git#branch=main,subdir=proto'; else echo "no origin/main yet; skipping buf breaking"; fi
	$(MAKE) proto-gen && git diff --exit-code -- api/internal/gen

proto-gen: ## buf generate (Go stubs; Rust stubs build via tonic-build)
	cd proto && buf generate

# ── deploy/ ───────────────────────────────────────────────────────────────────
deploy-verify: ## helm lint + kubeconform + kustomize build
	@test -f deploy/helm/snatcharr/Chart.yaml || { echo "deploy/helm not scaffolded yet; skipping"; exit 0; }
	helm lint deploy/helm/snatcharr
	helm template snatcharr deploy/helm/snatcharr | kubeconform -strict -ignore-missing-schemas -summary
	@if [ -f deploy/kustomize/kind/kustomization.yaml ]; then kustomize build --enable-helm deploy/kustomize/kind >/dev/null; fi

# ── governance (praetor) ──────────────────────────────────────────────────────
governance-verify: ## compile-context --verify, audit, state audit, flavor audit (go-service)
	$(PRAETORCTL) compile-context --verify
	$(PRAETORCTL) audit
	$(PRAETORCTL) state audit .
	$(PRAETORCTL) flavor audit --flavor=go-service .

hooks: ## Install git hooks
	lefthook install

setup: hooks ## First-time developer setup
	$(PRAETORCTL) state init --if-absent .
	@echo "Now run: make dev"

dev: ## Local stack: postgres (compose) + api (air) + worker (cargo watch) + web (vite)
	docker compose -f deploy/compose/docker-compose.yml up -d postgres
	@echo "Run in three terminals:"
	@echo "  cd api    && air -c ../tools/air.toml"
	@echo "  cd worker && cargo watch -x 'run -p snatch-worker'"
	@echo "  cd web    && pnpm dev"
