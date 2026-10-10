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
GOMODCACHE       = $(subst \,/,$(shell go env GOMODCACHE))
GOLUSORIS_VER    = $(shell go list -m -f '{{.Version}}' github.com/golusoris/golusoris)
GOLUSORIS_SHARED = $(GOMODCACHE)/github.com/golusoris/golusoris@$(GOLUSORIS_VER)/tools/Makefile.shared

.PHONY: help verify-all api-verify worker-verify web-verify web-e2e proto-verify deploy-verify governance-verify \
	images kind-up kind-deploy kind-down \
        api-gen gosec-install web-gen proto-gen dev hooks setup

help: ## Show this help
	@grep -hE '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-18s %s\n", $$1, $$2}'

verify-all: governance-verify proto-verify api-verify worker-verify web-verify deploy-verify ## Every gate (praetor universal verification)
	@echo "All SnatchArr verification gates passed."

# ── api/ (Go, golusoris) ──────────────────────────────────────────────────────
api-verify: ## golangci-lint + go fix + gosec + govulncheck + go test -race + generated-code drift + spectral
	@if [ ! -f go.mod ]; then echo "go.mod not scaffolded yet; skipping"; exit 0; fi
	$(MAKE) -C api -f "$(GOLUSORIS_SHARED)" ci GOLANGCI_CONFIG=../.golangci.yml GOSEC="gosec -exclude-generated -conf ../.gosec.json"
	cd api
	go fix -diff ./...
	go generate ./...
	git diff --exit-code -- internal/build internal/gen internal/store/sqlcgen
	cd ..
	# Every gosec finding in generated code is annotated, and every annotation still has its finding.
	go run ./tools/gennosec check
	$(MAKE) api-cover
	if [ -f api/openapi/openapi.yaml ]; then
	  npx --yes @stoplight/spectral-cli@6.15.0 lint api/openapi/openapi.yaml --ruleset tools/spectral.yaml --fail-severity warn
	fi

api-gen: ## ogen + sqlc generation inside api/
	cd api && go generate ./...

# gosec v2.29.0, the latest release, pins golang.org/x/tools v0.49.0, which cannot read the export
# data Go 1.27.2 writes (version 5; x/tools reads it from v0.50.0), so it fails to load every
# package. Build the release against a newer x/tools in a throwaway module until a gosec release
# carries one. CI (ci.yml, security.yml) installs gosec through this target.
GOSEC_VERSION ?= v2.29.0
GOSEC_XTOOLS  ?= v0.50.0
gosec-install: ## Install gosec built against an x/tools that reads Go 1.27.2 export data
	tmp=$$(mktemp -d)
	trap 'rm -rf "$$tmp"' EXIT
	cd "$$tmp"
	go mod init gosec-build
	go get github.com/securego/gosec/v2/cmd/gosec@$(GOSEC_VERSION) golang.org/x/tools@$(GOSEC_XTOOLS)
	go install github.com/securego/gosec/v2/cmd/gosec

# Ratchet: raise as store/handler integration tests land (target 70, HISS-15). Never lower.
API_COVER_MIN ?= 28
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
	@if [ ! -f Cargo.toml ]; then echo "Cargo workspace not scaffolded yet; skipping"; exit 0; fi
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
	# gosec findings of generated code carry one `#nosec <rule> -- <reason>` each (tools/gennosec).
	go run ./tools/gennosec apply

# ── deploy/ ───────────────────────────────────────────────────────────────────
# kubeconform binary if installed, else the same pinned image CI uses.
KUBECONFORM ?= $(shell command -v kubeconform >/dev/null 2>&1 && echo kubeconform || echo "docker run --rm -i ghcr.io/yannh/kubeconform:v0.7.0")
deploy-verify: ## helm lint --strict + kubeconform (default and all-features renders) + kustomize build
	helm lint --strict deploy/helm/snatcharr
	helm template snatcharr deploy/helm/snatcharr | $(KUBECONFORM) -strict -ignore-missing-schemas -summary
	helm template snatcharr deploy/helm/snatcharr 	  --set networkPolicy.enabled=true --set api.ingress.enabled=true --set serviceMonitor.enabled=true 	  --set worker.autoscaling.enabled=true --set api.pdb.enabled=true --set worker.pdb.enabled=true 	  --set configarr.enabled=true --set configarr.existingConfigMap=cfg --set configarr.existingSecret=sec 	  | $(KUBECONFORM) -strict -ignore-missing-schemas -summary
	kustomize build --enable-helm deploy/kustomize/kind >/dev/null
	# No egress rule may have an empty `to`, which allows every destination (#120).
	netpol=$$(helm template snatcharr deploy/helm/snatcharr --set networkPolicy.enabled=true --set-json 'networkPolicy.arrCidrs=[]')
	if printf '%s\n' "$$netpol" | grep -A1 -E '^ *- to:( *\[\])?$$' | grep -qE '^ *(- to: *)?\[\]$$'; then
	  echo "networkPolicy: an egress rule with an empty 'to' allows every destination" >&2; exit 1
	fi

images: ## Build both images locally (tag dev)
	docker build -t snatcharr-api:dev -f Dockerfile .
	docker build -t snatcharr-worker:dev -f worker/Dockerfile .

# kind runs only inside an incus VM (tools/incus-kind-vm.sh, #61): its kubelet writes host
# kernel settings (vm.overcommit_memory, kernel.panic) when it runs in host Docker. The host
# reaches the API server at the VM's address through a dedicated kubeconfig.
KIND_CLUSTER    ?= snatcharr
KIND_VM         ?= snatcharr-kind
KIND_VM_SCRIPT   = SNATCHARR_KIND_VM=$(KIND_VM) tools/incus-kind-vm.sh
KIND_KUBECONFIG ?= $(CURDIR)/.kind/$(KIND_CLUSTER).kubeconfig
KIND_KUBECTL     = kubectl --kubeconfig $(KIND_KUBECONFIG)
CNPG_VERSION    ?= 1.28.0
kind-up: images ## incus VM + kind cluster + CloudNativePG operator + SnatchArr (deploy/kustomize/kind)
	$(KIND_VM_SCRIPT) create
	addr=$$($(KIND_VM_SCRIPT) address)
	if ! incus exec $(KIND_VM) -- kind get clusters | grep -qx $(KIND_CLUSTER); then
	  { cat deploy/kustomize/kind/cluster.yaml; printf 'networking:\n  apiServerAddress: "%s"\n  apiServerPort: 6443\n' "$$addr"; } |
	    incus exec $(KIND_VM) -- kind create cluster --name $(KIND_CLUSTER) --config - --wait 120s
	fi
	mkdir -p $(dir $(KIND_KUBECONFIG))
	(umask 077; incus exec $(KIND_VM) -- kind get kubeconfig --name $(KIND_CLUSTER) > $(KIND_KUBECONFIG))
	$(KIND_KUBECTL) apply --server-side -f https://raw.githubusercontent.com/cloudnative-pg/cloudnative-pg/release-$(shell echo $(CNPG_VERSION) | cut -d. -f1,2)/releases/cnpg-$(CNPG_VERSION).yaml
	$(KIND_KUBECTL) -n cnpg-system rollout status deploy/cnpg-controller-manager --timeout=180s
	$(MAKE) kind-deploy

kind-deploy: ## Load dev images into the VM's kind cluster and apply the kind overlay
	for img in snatcharr-api:dev snatcharr-worker:dev; do
	  docker save $$img | incus exec $(KIND_VM) -- docker load
	  incus exec $(KIND_VM) -- kind load docker-image $$img --name $(KIND_CLUSTER)
	done
	kustomize build --enable-helm deploy/kustomize/kind | $(KIND_KUBECTL) apply --server-side -f -
	$(KIND_KUBECTL) -n snatcharr rollout status deploy/snatcharr-api --timeout=300s
	@echo "UI: $(KIND_KUBECTL) -n snatcharr port-forward svc/snatcharr-api 8080:8080"

kind-down: ## Delete the kind VM, its profile and the kubeconfig
	$(KIND_VM_SCRIPT) destroy
	rm -f $(KIND_KUBECONFIG)

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
	echo "  cargo watch -x 'run -p snatch-worker'"
	echo "  cd web    && pnpm dev"
