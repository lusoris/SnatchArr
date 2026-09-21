# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# SnatchArr monorepo verification entrypoint. `make verify-all` is the universal gate praetor
# audits; every component gate below is also runnable on its own.
#
# Recipes run as ONE shell (-e: first failing command aborts the recipe), so a component that
# is not scaffolded yet can `exit 0` early and the rest of the recipe is skipped.

SHELL := /bin/sh
.SHELLFLAGS := -ec
.ONESHELL:
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
	@if [ ! -f api/go.mod ]; then echo "api/ not scaffolded yet; skipping"; exit 0; fi
	$(MAKE) -C api -f "$(GOLUSORIS_SHARED)" ci GOLANGCI_CONFIG=../.golangci.yml
	cd api
	gosec -quiet -conf ../.gosec.json -exclude-generated ./...
	go generate ./...
	git diff --exit-code -- internal/build internal/gen
	cd ..
	$(MAKE) api-cover
	if [ -f api/openapi/openapi.yaml ]; then
	  npx --yes @stoplight/spectral-cli@6.15.0 lint api/openapi/openapi.yaml --ruleset tools/spectral.yaml --fail-severity warn
	fi

api-gen: ## ogen + sqlc generation inside api/
	cd api && go generate ./...

# Ratchet: raise as store/handler integration tests land (target 70, HISS-15). Never lower.
API_COVER_MIN ?= 20
api-cover: ## Coverage gate on hand-written packages (generated code excluded)
	cd api
	mkdir -p tmp
	go test -count=1 -covermode=atomic -coverprofile=tmp/cover.out ./... >/dev/null
	grep -vE 'internal/(build|gen)/|internal/store/sqlcgen/' tmp/cover.out > tmp/cover.filtered.out
	pct=$$(go tool cover -func=tmp/cover.filtered.out | awk '/^total:/ {gsub("%","",$$3); print $$3}')
	echo "coverage (non-generated): $${pct}% (min $(API_COVER_MIN)%)"
	awk -v p="$$pct" -v m="$(API_COVER_MIN)" 'BEGIN { exit (p+0 < m+0) ? 1 : 0 }'

# ── worker/ (Rust) ────────────────────────────────────────────────────────────
worker-verify: ## cargo fmt/clippy(-D warnings)/audit/deny/test
	@if [ ! -f worker/Cargo.toml ]; then echo "worker/ not scaffolded yet; skipping"; exit 0; fi
	cd worker
	cargo fmt --all --check
	cargo clippy --workspace --all-targets --locked -- -D warnings
	cargo audit
	cargo deny check
	cargo test --workspace --locked

# ── web/ (SvelteKit + sveltesentio) ───────────────────────────────────────────
web-verify: ## pnpm lint/typecheck/test(+coverage)/build + OpenAPI type drift
	@if [ ! -f web/package.json ]; then echo "web/ not scaffolded yet; skipping"; exit 0; fi
	cd web
	pnpm install --frozen-lockfile
	pnpm gen:api
	git diff --exit-code -- src/lib/api/schema.d.ts
	pnpm lint
	pnpm typecheck
	pnpm test -- --coverage
	pnpm build

web-gen: ## OpenAPI -> src/lib/api/schema.d.ts
	cd web && pnpm gen:api

web-e2e: ## Playwright + axe-core (zero violations; never suppressed)
	cd web && pnpm test:e2e

# ── proto/ ────────────────────────────────────────────────────────────────────
proto-verify: ## buf lint + breaking (vs main) + generation drift
	@if [ ! -f proto/buf.yaml ]; then echo "proto/ not scaffolded yet; skipping"; exit 0; fi
	cd proto
	buf lint
	if git rev-parse --verify -q origin/main >/dev/null 2>&1; then
	  buf breaking --against '../.git#branch=main,subdir=proto'
	else
	  echo "no origin/main yet; skipping buf breaking"
	fi
	cd ..
	$(MAKE) proto-gen
	git diff --exit-code -- api/internal/gen

proto-gen: ## buf generate (Go stubs; Rust stubs build via tonic-build)
	cd proto && buf generate
	cd ..
	# HISS-09 wants a SAFETY proof before every unsafe call; protoc-gen-go emits two. Deterministic post-pass.
	for f in $$(find api/internal/gen -name '*.pb.go'); do
	  sed -i -E 's|^([[:space:]]*)(.*unsafe\.Slice\(unsafe\.StringData\(.*)$$|\1// SAFETY: protoc-gen-go reads immutable string bytes of the raw descriptor; never written or retained past the call.\n\1\2|' "$$f"
	done

# ── deploy/ ───────────────────────────────────────────────────────────────────
deploy-verify: ## helm lint + kubeconform + kustomize build
	@if [ ! -f deploy/helm/snatcharr/Chart.yaml ]; then echo "deploy/helm not scaffolded yet; skipping"; exit 0; fi
	helm lint deploy/helm/snatcharr
	helm template snatcharr deploy/helm/snatcharr | kubeconform -strict -ignore-missing-schemas -summary
	if [ -f deploy/kustomize/kind/kustomization.yaml ]; then
	  kustomize build --enable-helm deploy/kustomize/kind >/dev/null
	fi

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
	echo "  cd api    && air -c ../tools/air.toml"
	echo "  cd worker && cargo watch -x 'run -p snatch-worker'"
	echo "  cd web    && pnpm dev"
