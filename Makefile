# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# SnatchArr monorepo verification entrypoint. `make verify-all` is the universal gate praetor
# audits; every component gate below is also runnable on its own.
#
# Recipes run as ONE shell (-e: first failing command aborts the recipe). Without pipefail a
# pipe's status is its last command's, so no gate pipes a check's input from a command that can
# fail; it writes that input to a file first.
-include .config/praetor/engine.mk

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

.PHONY: help verify-all api-verify worker-verify worker-cover e2e-smoke e2e-image-smoke web-verify web-e2e web-e2e-api proto-verify deploy-verify governance-verify hooks-verify contracts-verify workflows-verify workflows-lint secret-scan \
	images kind-up kind-deploy kind-down \
        api-gen api-gosec gosec-install govulncheck-install tools-verify tools-lint tools-test tools-gosec web-gen proto-gen dev hooks setup

help: ## Show this help
	@grep -hE '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-18s %s\n", $$1, $$2}'

verify-all: governance-verify hooks-verify workflows-verify secret-scan proto-verify api-verify worker-verify web-verify deploy-verify contracts-verify ## Every gate (praetor universal verification)
	@echo "All SnatchArr verification gates passed."

# ── api/ (Go, golusoris) ──────────────────────────────────────────────────────
api-verify: ## golangci-lint + go fix + gosec + govulncheck + go test -race + generated-code drift + spectral + tools-verify
	# go.mod and go.sum hold exactly what the code needs: prints the missing changes and fails.
	go mod tidy -diff
	# The shared `ci` goal is `lint sec test`; its gosec step runs inside api/, where gosec loads no
	# package (see api-gosec), so the three other goals run here and gosec runs from the module root.
	$(MAKE) -C api -f "$(GOLUSORIS_SHARED)" lint vuln test GOLANGCI_CONFIG=../.golangci.yml
	$(MAKE) api-gosec
	cd api
	go fix -diff ./...
	go generate ./...
	git diff --exit-code -- internal/build internal/gen internal/store/sqlcgen
	cd ..
	# Every gosec finding in generated code is annotated, and every annotation still has its finding.
	go run ./tools/gennosec check
	$(MAKE) tools-verify
	$(MAKE) api-cover
	sh tools/spectral/lint_test.sh
	sh tools/spectral/lint.sh api/openapi/openapi.yaml

api-gen: ## ogen + sqlc generation inside api/
	cd api && sqlc generate && go generate ./...

# gosec loads packages only when it starts in the directory that holds go.mod. Started in api/ it
# lists the directories, checks no file, prints "Files  : 0" and exits 0. This target is the one
# place that runs it (api-verify, ci.yml, security.yml): from the module root, over every package,
# and it fails when the summary reports no scanned file. The SARIF report feeds code scanning.
GOSEC_SARIF ?= api/tmp/gosec.sarif
GOSEC_PKG   ?= ./...
GOSEC_TAGS  ?=
api-gosec: ## gosec over the whole module (zero exclusions); fails on a finding or when no file was scanned
	mkdir -p "$(dir $(GOSEC_SARIF))"
	log=$$(mktemp)
	trap 'rm -f "$$log"' EXIT
	status=0
	gosec -conf .gosec.json -exclude-generated $(if $(GOSEC_TAGS),-tags $(GOSEC_TAGS)) -fmt sarif -out "$(GOSEC_SARIF)" -stdout -verbose text $(GOSEC_PKG) >"$$log" 2>&1 || status=$$?
	cat "$$log"
	if ! grep -q -E '^[[:space:]]*Files[[:space:]]*:[[:space:]]*[1-9]' "$$log"; then
	  echo "api-gosec: gosec scanned no file ($(GOSEC_PKG), started in $$(pwd)); a scan of nothing is not a pass" >&2
	  exit 1
	fi
	test -s "$(GOSEC_SARIF)"
	exit $$status

# gosec v2.29.0, the latest release, pins golang.org/x/tools v0.49.0, which cannot read the export
# data Go 1.27.2 writes (version 5; x/tools reads it from v0.50.0), so it fails to load every
# package. tools/go/go.mod builds it against a newer x/tools and pins govulncheck beside it; CI
# (ci.yml, security.yml) and the devcontainer install both through these targets, never @latest.
# SECURITY_TOOLS_MOD names another module directory, for the planted old x/tools case only.
SECURITY_TOOLS_MOD ?= tools/go
gosec-install: ## Install gosec at the tools/go/go.mod pins (x/tools reads Go 1.27.2 export data)
	cd "$(SECURITY_TOOLS_MOD)"
	go install github.com/securego/gosec/v2/cmd/gosec

govulncheck-install: ## Install govulncheck at the tools/go/go.mod pin
	cd "$(SECURITY_TOOLS_MOD)"
	go install golang.org/x/vuln/cmd/govulncheck

# Ratchet: raise as store/handler integration tests land (target 70, HISS-15). Never lower.
# 41.8 % measured on main at #198.
API_COVER_MIN ?= 41
api-cover: ## Coverage gate on hand-written packages (generated code excluded)
	sh tools/cover/floor_test.sh
	cd api
	mkdir -p tmp
	go test -count=1 -covermode=atomic -coverprofile=tmp/cover.out ./... >/dev/null
	grep -vE 'internal/(build|gen)/|internal/store/sqlcgen/' tmp/cover.out > tmp/cover.filtered.out
	pct=$$(go tool cover -func=tmp/cover.filtered.out | awk '/^total:/ {gsub("%","",$$3); print $$3}')
	echo "coverage (non-generated): $${pct}% (min $(API_COVER_MIN)%)"
	sh ../tools/cover/floor.sh "$$pct" "$(API_COVER_MIN)"

# ── tools/ (Go, root module) ─────────────────────────────────────────────────
# The gate code under tools/ gets the gate it applies to api/: the same .golangci.yml, go vet,
# go test -race and gosec. tools/apicompat/gate/main.go builds only with TOOLS_TAG, so each step
# runs once without the tag and once with it.
GOLANGCI   ?= golangci-lint
TOOLS_PKG  ?= ./tools/...
TOOLS_TAG  ?= apicompatgate
TOOLS_LIST ?= exceptions.yaml
tools-verify: tools-lint tools-test tools-gosec ## golangci-lint + go vet + go test -race + gosec over tools/

# The tagged run fails on no finding by itself: it writes every finding to a report, and
# tools/lintexcept accepts the report only when each finding has a dated exception in
# TOOLS_LIST that still holds and each exception still has its finding. No file is excluded in
# .golangci.yml, so the list is the one place that excuses a finding.
tools-lint: ## golangci-lint over tools/; a finding needs a dated exception in exceptions.yaml
	$(GOLANGCI) run --config .golangci.yml $(TOOLS_PKG)
	report=$$(mktemp)
	trap 'rm -f "$$report"' EXIT
	$(GOLANGCI) run --config .golangci.yml --build-tags $(TOOLS_TAG) --issues-exit-code 0 \
	  --max-issues-per-linter 0 --max-same-issues 0 --uniq-by-line=false \
	  --output.json.path "$$report" $(TOOLS_PKG)
	go run ./tools/lintexcept check -list "$(TOOLS_LIST)" -report "$$report"

tools-test: ## go vet + go test -race over tools/
	go vet $(TOOLS_PKG)
	go vet -tags $(TOOLS_TAG) $(TOOLS_PKG)
	go test -race -count=1 -timeout=120s $(TOOLS_PKG)

# api-gosec scans tools/ without the tag (it runs over ./...); this is the scan with it.
tools-gosec: ## gosec over tools/ built with the apicompatgate tag
	$(MAKE) api-gosec GOSEC_PKG=$(TOOLS_PKG) GOSEC_TAGS=$(TOOLS_TAG) GOSEC_SARIF=api/tmp/gosec-tools.sarif

# ── worker/ (Rust) ────────────────────────────────────────────────────────────
worker-verify: ## cargo fmt/clippy(-D warnings)/audit/deny/test
	cargo fmt --all --check
	cargo clippy --workspace --all-targets --locked -- -D warnings
	cargo audit
	cargo deny check
	cargo test --workspace --locked

# Worker line coverage floor; ratchet up as tests land (89.83 % measured on main at #198). CI runs it;
# locally it needs cargo-llvm-cov and the llvm-tools rustup component (or LLVM_COV and
# LLVM_PROFDATA pointing at a system LLVM matching rustc's).
WORKER_COVER_MIN ?= 89
# The floor goes through tools/cover/floor.sh, as api-cover's does: --fail-under-lines exits 1
# without a message, the same status as a report over missing data.
worker-cover: ## cargo-llvm-cov line coverage gate for the Rust workspace
	sh tools/cover/floor_test.sh
	cargo llvm-cov --workspace --locked --summary-only
	pct=$$(cargo llvm-cov report --json --summary-only | jq -r '.data[0].totals.lines.percent')
	echo "worker line coverage: $${pct}% (min $(WORKER_COVER_MIN)%)"
	sh tools/cover/floor.sh "$$pct" "$(WORKER_COVER_MIN)"

# ── end-to-end smoke (#19) ────────────────────────────────────────────────────
E2E_BIN := api/tmp/e2e
# The Postgres the integration tests pin (storetest), through the Docker Hub mirror.
E2E_POSTGRES ?= mirror.gcr.io/library/postgres:18-alpine@sha256:77f585114c32fbca283dc835b0596f4e52b51b4c6662d7810b2f4084f60a1873
E2E_API_IMAGE ?= snatcharr-api:e2e
E2E_WORKER_IMAGE ?= snatcharr-worker:e2e
e2e-smoke: ## Postgres + api + two workers + fakearr end to end (needs Docker unless SMOKE_DSN is set)
	mkdir -p $(E2E_BIN)
	go build -o $(E2E_BIN)/snatcharr ./api/cmd/snatcharr
	go build -o $(E2E_BIN)/fakearr ./api/cmd/fakearr
	cargo build --locked -p snatch-worker
	E2E_POSTGRES=$(E2E_POSTGRES) tools/e2e/postgres.sh \
	  env SMOKE_API_BIN=$(E2E_BIN)/snatcharr SMOKE_FAKEARR_BIN=$(E2E_BIN)/fakearr \
	  SMOKE_WORKER_BIN=target/debug/snatch-worker tools/e2e/smoke.sh

e2e-image-smoke: ## Postgres + api image + worker image + fakearr end to end; not in verify-all (slow)
	mkdir -p $(E2E_BIN)
	go build -o $(E2E_BIN)/fakearr ./api/cmd/fakearr
	if [ "$${SMOKE_SKIP_BUILD:-0}" != "1" ]; then \
	  docker build -t $(E2E_API_IMAGE) -f Dockerfile . ; \
	  docker build -t $(E2E_WORKER_IMAGE) -f worker/Dockerfile . ; \
	fi
	E2E_POSTGRES=$(E2E_POSTGRES) tools/e2e/postgres.sh \
	  env SMOKE_API_IMAGE=$(E2E_API_IMAGE) SMOKE_WORKER_IMAGE=$(E2E_WORKER_IMAGE) \
	  SMOKE_FAKEARR_BIN=$(E2E_BIN)/fakearr tools/e2e/smoke.sh

# ── web/ (SvelteKit + sveltesentio) ───────────────────────────────────────────
web-verify: ## pnpm lint/typecheck/test(+coverage)/build + OpenAPI type drift
	cd web
	pnpm install --frozen-lockfile
	pnpm gen:api
	git diff --exit-code -- src/lib/api/schema.d.ts
	pnpm lint
	pnpm typecheck
	pnpm test -- --coverage
	pnpm build
	python3 ../tools/ci/web_strings_scan.py

web-gen: ## OpenAPI -> src/lib/api/schema.d.ts
	cd web && pnpm gen:api

web-e2e: ## Playwright + axe-core (zero violations; never suppressed)
	cd web && pnpm test:e2e

web-e2e-api: ## Postgres + api + fakearr + web e2e Playwright axe sweep (needs Docker unless SMOKE_DSN is set)
	mkdir -p $(E2E_BIN)
	go build -o $(E2E_BIN)/snatcharr ./api/cmd/snatcharr
	go build -o $(E2E_BIN)/fakearr ./api/cmd/fakearr
	(cd web && pnpm install --frozen-lockfile && pnpm exec playwright install chromium)
	E2E_POSTGRES=$(E2E_POSTGRES) tools/e2e/postgres.sh \
	  env SMOKE_API_BIN=$(E2E_BIN)/snatcharr SMOKE_FAKEARR_BIN=$(E2E_BIN)/fakearr \
	  tools/e2e/web-e2e-api.sh

# ── proto/ ────────────────────────────────────────────────────────────────────
proto-verify: ## buf lint + breaking (vs main) + generation drift
	cd proto
	buf lint
	# Against origin/main, which must exist: without it the check was skipped and passed (A2-05).
	if ! git rev-parse --verify -q origin/main >/dev/null; then
	  echo "proto-verify: no origin/main to check breaking changes against; run git fetch origin main" >&2; exit 1
	fi
	buf breaking --against '../.git#ref=origin/main,subdir=proto'
	cd ..
	$(MAKE) proto-gen
	git diff --exit-code -- api/internal/gen

proto-gen: ## buf generate (Go stubs; Rust stubs build via tonic-build)
	cd proto && buf generate
	cd ..
	# HISS-09 wants a SAFETY proof before every unsafe call; protoc-gen-go emits two. Deterministic post-pass.
	# If protoc-gen-go changes shape and the sed matches nothing, the CI job semgrep fails on the
	# regenerated file (rule unsafe-requires-safety-proof in .semgrep.yml; gap audit A3-04).
	for f in $$(find api/internal/gen -name '*.pb.go'); do
	  sed -i -E 's|^([[:space:]]*)(.*unsafe\.Slice\(unsafe\.StringData\(.*)$$|\1// SAFETY: protoc-gen-go reads immutable string bytes of the raw descriptor; never written or retained past the call.\n\1\2|' "$$f"
	  sed -i -E 's|^([[:space:]]*)(unsafe "unsafe")$$|\1// SAFETY: imported only for the raw-descriptor views proved below.\n\1\2|' "$$f"
	done
	# gosec findings of generated code carry one `#nosec <rule> -- <reason>` each (tools/gennosec).
	go run ./tools/gennosec apply

# ── deploy/ ───────────────────────────────────────────────────────────────────
# kubeconform binary if installed, else the same pinned image CI uses.
KUBECONFORM ?= $(shell command -v kubeconform >/dev/null 2>&1 && echo kubeconform || echo "docker run --rm -i ghcr.io/yannh/kubeconform:v0.7.0")
deploy-verify: ## helm unittest + render.sh + chart_checks.sh planted cases + helm lint --strict + kubeconform (default and all-features renders) + kustomize build
	if ! helm plugin list 2>/dev/null | grep -E -q '^unittest[[:space:]]'; then \
	  echo "deploy-verify: helm plugin 'unittest' is missing; install helm-unittest (https://github.com/helm-unittest/helm-unittest)" >&2; exit 1; \
	fi
	helm unittest deploy/helm/snatcharr
	sh tools/deploy/render_test.sh
	helm lint --strict deploy/helm/snatcharr
	render=$$(mktemp -d)
	trap 'rm -rf "$$render"' EXIT
	# Rendered to a file first: a pipe into kubeconform passed a failing render (A2-01).
	tools/deploy/render.sh "$$render/default.yaml" snatcharr deploy/helm/snatcharr
	$(KUBECONFORM) -strict -ignore-missing-schemas -summary <"$$render/default.yaml"
	tools/deploy/render.sh "$$render/all.yaml" snatcharr deploy/helm/snatcharr \
	  --set networkPolicy.enabled=true --set api.ingress.enabled=true --set serviceMonitor.enabled=true \
	  --set worker.autoscaling.enabled=true --set api.pdb.enabled=true --set worker.pdb.enabled=true \
	  --set configarr.enabled=true --set configarr.existingConfigMap=cfg --set configarr.existingSecret=sec \
	  --set-json 'networkPolicy.arrPeers=[{"namespaceSelector":{"matchLabels":{"kubernetes.io/metadata.name":"media"}}},{"podSelector":{"matchLabels":{"app.kubernetes.io/name":"lidarr"}},"ports":[{"port":8686,"protocol":"TCP"}]}]'
	$(KUBECONFORM) -strict -ignore-missing-schemas -summary <"$$render/all.yaml"
	kustomize build --enable-helm deploy/kustomize/kind >/dev/null
	# Chart checks with their planted cases first (A3-01): no egress rule with an empty `to`
	# (#120), an arrPeers entry with `ports` reachable on those ports only (#145), Chart.yaml
	# release-please markers and image annotation (#122).
	sh tools/deploy/chart_checks_test.sh
	sh tools/deploy/chart_checks.sh netpol "$$render/all.yaml"
	tools/deploy/render.sh "$$render/no-peers.yaml" snatcharr deploy/helm/snatcharr \
	  --set networkPolicy.enabled=true --set-json 'networkPolicy.arrCidrs=[]'
	sh tools/deploy/chart_checks.sh netpol "$$render/no-peers.yaml" "2 with a to, 0 of them without ports, 0 with an empty to"
	# With one arrPeers entry that has `ports` as the only *arr peer, both policies hold two egress
	# rules with a `to` (database or API, and the peer), and each of the four carries `ports`.
	tools/deploy/render.sh "$$render/peer.yaml" snatcharr deploy/helm/snatcharr \
	  --set networkPolicy.enabled=true --set-json 'networkPolicy.arrCidrs=[]' \
	  --set-json 'networkPolicy.arrPeers=[{"podSelector":{"matchLabels":{"app.kubernetes.io/name":"lidarr"}},"ports":[{"port":8686,"protocol":"TCP"}]}]'
	sh tools/deploy/chart_checks.sh netpol "$$render/peer.yaml" "4 with a to, 0 of them without ports, 0 with an empty to"
	app=$$(helm show chart deploy/helm/snatcharr | sed -n 's/^appVersion: *//p' | tr -d "\"'")
	sh tools/deploy/chart_checks.sh chart deploy/helm/snatcharr/Chart.yaml "$$app"

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
# ── upstream contracts ────────────────────────────────────────────────────────
# The hand-written clients (worker/crates/arr-client, api/internal/seerrclient) against the OpenAPI
# specs their upstream projects publish, pinned in tools/upstreamspec/specs.yaml. The specs are
# downloaded into SPEC_CACHE, which is never committed and never enters an image build context
# (docs/upstream-contracts.md). A cold cache needs the network.
#
# A test command that matches no test exits 0, so the last step asks for the receipt each
# contract test leaves when it passed against the pinned commit.
SPEC_CACHE ?= .cache/upstream-specs
contracts-verify: ## Hand-written clients against the pinned upstream OpenAPI specs
	go test -count=1 ./tools/upstreamspec/...
	go run ./tools/upstreamspec fetch -cache "$(SPEC_CACHE)"
	go run ./tools/upstreamspec guard -cache "$(SPEC_CACHE)"
	export SNATCHARR_SPEC_CACHE="$(abspath $(SPEC_CACHE))"
	go test -count=1 -run '^TestUpstreamContract$$' ./api/internal/seerrclient/
	cargo test --locked -p arr-client -- --ignored upstream_contract
	go run ./tools/upstreamspec receipts -cache "$(SPEC_CACHE)"

governance-verify: ## compile-context --verify, audit, state audit, flavor audit (go-service)
	$(PRAETORCTL) compile-context --verify
	$(PRAETORCTL) audit
	$(PRAETORCTL) state audit .
	$(PRAETORCTL) flavor audit --flavor=go-service .

hooks-verify: ## The commit-message policy against its accepted and refused cases
	sh .config/hooks/commit-msg_test.sh

secret-scan: ## gitleaks over every local ref, as CI job secret-scan (pinned release, cached in .cache/)
	sh tools/ci/gitleaks_scan.sh .cache/gitleaks

workflows-verify: ## The rule of "CI success" and the Security Scan issue step, against their cases
	sh tools/ci/status_test.sh
	sh tools/ci/security_issue_test.sh

# Not part of verify-all: needs Docker, the network and a forge token (GH_TOKEN, read access).
# The CI job "workflows" runs it on every pull request.
workflows-lint: ## actionlint + pinned actions (zizmor) on every workflow file, with planted cases
	bash tools/ci/workflow-check_test.sh
	bash tools/ci/workflow-check.sh

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
