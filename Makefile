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

.PHONY: help verify-all api-verify worker-verify worker-cover e2e-smoke e2e-image-smoke e2e-soak e2e-compose-smoke e2e-compose-pg-upgrade e2e-compose-app-upgrade e2e-dev-compose-smoke dlclients-live arr-forceimport-live integrations-live e2e-cnpg-pg-upgrade e2e-cnpg-backup-restore e2e-chart-install web-verify web-e2e web-e2e-api web-visual web-visual-update proto-verify deploy-verify chart-package governance-verify hooks-verify contracts-verify workflows-verify workflows-lint release-e2e-check release-dogfood-check secret-scan py-lint docs-verify licences licences-check logos logos-check \
	images kind-up kind-deploy kind-keda-proof kind-down \
        api-gen api-gosec api-vuln gosec-install govulncheck-install sqlc-install tools-verify tools-lint tools-test tools-gosec web-gen proto-gen dev hooks setup api-openapi-breaking settings-parity settings-parity-check roadmap-check

help: ## Show this help
	@grep -hE '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-18s %s\n", $$1, $$2}'

verify-all: governance-verify hooks-verify workflows-verify secret-scan py-lint docs-verify proto-verify api-verify worker-verify web-verify deploy-verify contracts-verify licences-check logos-check roadmap-check ## Every gate (praetor universal verification)
	@echo "All SnatchArr verification gates passed."

# ── api/ (Go, golusoris) ──────────────────────────────────────────────────────
# How many api test packages run at once. About 24 of them boot a Postgres container each;
# at the default -p (one per core, 32 here) they all stop together, Docker kills 16 of them
# after the 10 s stop timeout, and the store upgrade test, whose two containers start in
# that window, took 89 s of the 120 s package budget. At -p 16 under the same host load
# (2026-10-31): 2 forced stops, upgrade test 24 s, whole run 77 s instead of 104 s.
API_TEST_PARALLEL ?= 16

api-verify: ## golangci-lint + go fix + gosec + govulncheck + go test -race + generated-code drift + spectral + tools-verify
	# go.mod and go.sum hold exactly what the code needs: prints the missing changes and fails.
	go mod tidy -diff
	# The shared `ci` goal is `lint sec test`; its gosec step runs inside api/, where gosec loads no
	# package (see api-gosec), so the three other goals run here and gosec runs from the module root.
	# The shared `vuln` goal runs whatever govulncheck the PATH holds; api-vuln runs the pinned one
	# that CI runs, and the scanners are installed at their pins first (#161).
	$(MAKE) -C api -f "$(GOLUSORIS_SHARED)" lint GOLANGCI_CONFIG=../.golangci.yml
	env GOFLAGS="$$(go env GOFLAGS) -p=$(API_TEST_PARALLEL)" $(MAKE) -C api -f "$(GOLUSORIS_SHARED)" test
	$(MAKE) api-vuln
	$(MAKE) gosec-install
	$(MAKE) api-gosec
	cd api
	go fix -diff ./...
	# go generate rewrites the ogen server (internal/build) and the sqlc queries
	# (internal/store/sqlcgen); proto-verify compares internal/gen. A generated file not yet
	# committed is drift as well: git diff does not see it.
	go generate ./...
	git diff --exit-code -- internal/build internal/store/sqlcgen
	if git ls-files --others --exclude-standard -- internal/build internal/store/sqlcgen | grep .; then
	  echo "api-verify: generated files above are not committed" >&2; exit 1
	fi
	cd ..
	# Every gosec finding in generated code is annotated, and every annotation still has its finding.
	go run ./tools/gennosec check
	$(MAKE) tools-verify
	$(MAKE) api-cover
	sh tools/spectral/lint_test.sh
	sh tools/spectral/lint.sh api/openapi/openapi.yaml
	$(MAKE) api-openapi-breaking
	go run ./tools/moduleroutes check -in api/openapi/openapi.yaml
	$(MAKE) settings-parity-check

api-openapi-breaking: ## OpenAPI breaking change check vs origin/main (HISS-14 append-only API)
	if ! git rev-parse --verify -q origin/main >/dev/null; then
	  echo "api-openapi-breaking: no origin/main to check breaking changes against; run git fetch origin main" >&2; exit 1
	fi
	base=$$(mktemp)
	trap 'rm -f "$$base"' EXIT
	git show origin/main:api/openapi/openapi.yaml >"$$base"
	sh tools/oasdiff/breaking_test.sh
	sh tools/oasdiff/breaking.sh "$$base" api/openapi/openapi.yaml "$$(git log -1 --format=%s)"

settings-parity: ## Generate docs/settings-parity.tsv from config registry
	go run ./api/cmd/settingsparity

settings-parity-check: ## Verify docs/settings-parity.tsv matches config registry
	go run ./api/cmd/settingsparity -check

api-gen: ## Regenerate the ogen server and the sqlc queries (go generate in api/, pinned versions)
	cd api && go generate ./...

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

# api-verify, ci.yml and security.yml scan through this target, so all three run the same pinned
# scanner and print its version (#161). It calls the binary govulncheck-install wrote, never the
# PATH lookup: a developer's older govulncheck earlier on the PATH does not take its place.
GO_INSTALL_DIR = $(or $(shell go env GOBIN),$(firstword $(subst :, ,$(shell go env GOPATH)))/bin)
api-vuln: govulncheck-install ## govulncheck at the tools/go/go.mod pin over api/, version printed first
	cd api
	"$(GO_INSTALL_DIR)/govulncheck" -version
	"$(GO_INSTALL_DIR)/govulncheck" ./...

sqlc-install: ## Install sqlc at the tools/go/go.mod pin, the version go generate runs
	cd tools/go
	go install github.com/sqlc-dev/sqlc/cmd/sqlc

# Ratchet: raise as store/handler integration tests land (target 70, HISS-15). Never lower.
# 41.8 % measured on main at #198.
API_COVER_MIN ?= 41
api-cover: ## Coverage gate on hand-written packages (generated code excluded)
	sh tools/cover/floor_test.sh
	sh tools/cover/gotest_test.sh
	cd api
	mkdir -p tmp
	sh ../tools/cover/gotest.sh tmp/cover-test.log -- go test -count=1 -covermode=atomic -coverprofile=tmp/cover.out ./...
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
TOOLS_ISSUE ?= 1226
tools-verify: tools-lint tools-test tools-gosec ## golangci-lint + go vet + go test -race + gosec over tools/

# The tagged run fails on no finding by itself: it writes every finding to a report, measured
# against .golangci.yml without its generated exceptions block. tools/ci/lint_exceptions.sh
# regenerates that block from the report with praetorctl at the pinned engine and fails on a new
# finding, on an expired entry whose finding is still there, and on an entry whose finding is
# gone. TOOLS_ISSUE is the cordanaLLM/praetor issue each generated entry names.
tools-lint: ## golangci-lint over tools/; a finding needs a dated entry in the generated block of .golangci.yml
	$(GOLANGCI) run --config .golangci.yml $(TOOLS_PKG)
	sh tools/ci/lint_exceptions_test.sh
	sh tools/ci/dependency_snapshot_test.sh
	work=$$(mktemp -d)
	trap 'rm -rf "$$work"' EXIT
	sh .config/lefthook/engine.sh linters exceptions strip-config --config .golangci.yml --out "$$work/golangci.yml"
	$(GOLANGCI) run --config "$$work/golangci.yml" --build-tags $(TOOLS_TAG) --issues-exit-code 0 \
	  --max-issues-per-linter 0 --max-same-issues 0 --uniq-by-line=false \
	  --output.json.path "$$work/report.json" $(TOOLS_PKG)
	sh tools/ci/lint_exceptions.sh .golangci.yml "$$work/report.json" $(TOOLS_ISSUE)

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

e2e-image-smoke: ## Postgres + api image + worker image + fakearr end to end, under the chart memory limits; not in verify-all (slow)
	# The memory check's cases first (C12-01): a limit it cannot read and a peak above 80 %
	# of the limit must be refused.
	bash tools/e2e/mem_limits_test.sh
	mkdir -p $(E2E_BIN)
	go build -o $(E2E_BIN)/fakearr ./api/cmd/fakearr
	if [ "$${SMOKE_SKIP_BUILD:-0}" != "1" ]; then \
	  docker build -t $(E2E_API_IMAGE) -f Dockerfile . ; \
	  docker build -t $(E2E_WORKER_IMAGE) -f worker/Dockerfile . ; \
	fi
	E2E_POSTGRES=$(E2E_POSTGRES) tools/e2e/postgres.sh \
	  env SMOKE_API_IMAGE=$(E2E_API_IMAGE) SMOKE_WORKER_IMAGE=$(E2E_WORKER_IMAGE) \
	  SMOKE_FAKEARR_BIN=$(E2E_BIN)/fakearr tools/e2e/smoke.sh

# The image smoke kept running for SOAK_SECONDS (C14-01): a missing run on sonarr and radarr
# every SOAK_ROUND_SECONDS, the gRPC connection rotated every SOAK_CONNECTION_AGE so the lease
# long-polls cross many rotations. Any failed worker API call, failed run or stale worker
# contact fails it (tools/e2e/smoke.sh, step j). Nightly in CI; not in verify-all (slow).
SOAK_SECONDS ?= 600
SOAK_CONNECTION_AGE ?= 15s
e2e-soak: ## Image smoke held for SOAK_SECONDS with frequent gRPC connection rotation (needs Docker)
	$(MAKE) e2e-image-smoke SOAK_SECONDS=$(SOAK_SECONDS) SMOKE_GRPC_MAX_CONNECTION_AGE=$(SOAK_CONNECTION_AGE) SMOKE_LEASE_WAIT_SECS=30

# The homelab bundle (#41) started as docs/install-compose.md says, on the images built here
# (the API and worker health checks need the `healthcheck` subcommand they carry). The planted
# cases first: an API health check that cannot pass, and a setup already completed; the smoke
# must refuse both. Not in verify-all (slow, needs Docker).
e2e-compose-smoke: ## Homelab compose bundle up from the docs: healthy services + first-run setup screen (needs Docker)
	if [ "$${SMOKE_SKIP_BUILD:-0}" != "1" ]; then \
	  docker build -t $(E2E_API_IMAGE) -f Dockerfile . ; \
	  docker build -t $(E2E_WORKER_IMAGE) -f worker/Dockerfile . ; \
	fi
	SMOKE_API_IMAGE=$(E2E_API_IMAGE) SMOKE_WORKER_IMAGE=$(E2E_WORKER_IMAGE) \
	  bash tools/e2e/planted.sh SMOKE_PLANT tools/e2e/compose_smoke.sh \
	  "unhealthy:did not turn healthy" "setup-done:want setup_complete false"

# A homelab install on the previous Postgres major moved to the pinned one by the steps of
# docs/install-compose.md, on the images built here: data read back table by table, setup
# still complete, the admin logs in. The planted cases skip one step each (moving the old data
# aside, the restore); the test must refuse both. Not in verify-all (slow, needs Docker).
e2e-compose-pg-upgrade: ## Homelab compose bundle: Postgres previous major -> pinned major upgrade with data read-back (needs Docker)
	if [ "$${SMOKE_SKIP_BUILD:-0}" != "1" ]; then \
	  docker build -t $(E2E_API_IMAGE) -f Dockerfile . ; \
	  docker build -t $(E2E_WORKER_IMAGE) -f worker/Dockerfile . ; \
	fi
	SMOKE_API_IMAGE=$(E2E_API_IMAGE) SMOKE_WORKER_IMAGE=$(E2E_WORKER_IMAGE) \
	  bash tools/e2e/planted.sh UPGRADE_PLANT tools/e2e/compose_pg_upgrade.sh \
	  "skip-move:the new postgres" "skip-restore:data read-back differs"

# A homelab install on the previous release moved to the candidate build: schema
# migrations advance, settings and data read back unchanged, the admin logs in and stored
# credentials decrypt. The planted cases delete a row, change a setting, start with the
# wrong crypto key, upgrade to old images, change an unread table, leave out the worker, delete
# the API key and replace the password hash; the test must refuse all eight. Not in verify-all
# (slow, needs Docker).
e2e-compose-app-upgrade: ## Homelab compose bundle: application upgrade from previous release with data read-back (needs Docker)
	if [ "$${SMOKE_SKIP_BUILD:-0}" != "1" ]; then \
	  docker build -t $(E2E_API_IMAGE) -f Dockerfile . ; \
	  docker build -t $(E2E_WORKER_IMAGE) -f worker/Dockerfile . ; \
	fi
	bash tools/e2e/schema_check_test.sh
	SMOKE_API_IMAGE=$(E2E_API_IMAGE) SMOKE_WORKER_IMAGE=$(E2E_WORKER_IMAGE) \
	  bash tools/e2e/planted.sh UPGRADE_PLANT tools/e2e/compose_app_upgrade.sh \
	  "drop-row:api read-back differs" \
	  "change-setting:settings differ" \
	  "wrong-crypto-key:stored key" \
	  "old-image:did not advance" \
	  "touch-table:data read-back differs" \
	  "no-worker:worker did not register" \
	  "drop-key:old API key" \
	  "reset-password:login with password"

# The development stack of deploy/compose (`make dev` with the arr and observability profiles,
# docs/metrics.md): Postgres healthy, Sonarr and Radarr answer, Prometheus scrapes the collector,
# Grafana has its data source and the provisioned dashboard. The planted cases mount an empty
# dashboard folder and point Prometheus at a closed port; the smoke must refuse both (gap audit
# C9-08). Not in verify-all (slow, needs Docker).
e2e-dev-compose-smoke: ## Dev compose stack with both profiles: services up, Prometheus target up, Grafana dashboard provisioned (needs Docker)
	bash tools/e2e/pull_retry_test.sh
	bash tools/e2e/planted.sh DEV_PLANT tools/e2e/dev_compose_smoke.sh \
	  "no-dashboards:grafana provisioned 0 dashboards" "bad-target:the otel-collector target is not up"

# The six download clients SnatchArr observes, each a real release (tools/e2e/dlclients),
# through the adapters production uses: reachable, version, empty queue, paused state, a wrong
# secret refused. The planted cases hand every client a wrong secret and claim a pause that never
# happened; the test must refuse both (gap audit C9-05). Not in verify-all (slow, needs Docker).
dlclients-live: ## Real qBittorrent, Transmission, Deluge, rTorrent, SABnzbd, NZBGet observed by the adapters (needs Docker)
	bash tools/e2e/pull_retry_test.sh
	bash tools/e2e/planted_test.sh
	bash tools/e2e/live_lib_test.sh
	bash tools/e2e/planted.sh SNATCHARR_DLCLIENTS_PLANT tools/e2e/dlclients_live.sh \
	  "secret:not reachable with the right secret" "pause:paused = false; the client was left with paused = true" \
	  "priority:priority capability mismatch"

# Rule id_matched (ADR-0013) on real Sonarr and Radarr (tools/e2e/forceimport): each grabs a
# release by ID under a title it cannot parse, and the production client force-imports it and
# reads the queue back. The planted cases (no audio track, only unmapped files, a second
# status line, no import sent) must each fail. Not in verify-all (slow, needs Docker and the
# metadata lookups of both apps).
arr-forceimport-live: ## Real Sonarr and Radarr: ID-matched release force-imported, planted cases refused (needs Docker)
	bash tools/e2e/pull_retry_test.sh
	bash tools/e2e/planted_test.sh
	bash tools/e2e/live_lib_test.sh
	bash tools/e2e/planted.sh SNATCHARR_FORCEIMPORT_PLANT tools/e2e/forceimport_live.sh \
	  "audio:no clean, mapped manual-import candidate" "unmapped:no clean, mapped manual-import candidate" \
	  "second-line:is not id-matched" "skip:is still in the queue"

# All supported integrations driven end to end against real application containers
# (tools/e2e/integrations): Sonarr, Radarr, Lidarr, Readarr, Whisparr v2, Whisparr v3,
# Prowlarr, Seerr, SABnzbd and qBittorrent. The planted cases (bad key, bad path, bad client
# credentials, bad instance credentials) must each fail. Not in verify-all (slow, needs Docker).
integrations-live: ## Real app containers: every integration driven end to end (needs Docker)
	bash tools/e2e/pull_retry_test.sh
	bash tools/e2e/planted_test.sh
	bash tools/e2e/live_lib_test.sh
	bash tools/e2e/planted.sh SNATCHARR_INTEGRATIONS_PLANT tools/e2e/integrations_live.sh \
	  "badkey:prowlarr test failed" "seerrpath:seerr test failed" \
	  "badclient:download client test failed" "badinstance:instance test failed"

# The chart on CloudNativePG moved from the previous Postgres major to the current one by the
# imageName change of docs/deployment.md, with data read-back and a planted upgrade without the
# image change. Needs a cluster with the operator: CI runs it in the deploy job on kind; on a
# workstation only inside a VM (no kind on the host). Not in verify-all.
e2e-cnpg-pg-upgrade: ## Chart on CloudNativePG: Postgres previous major -> current major upgrade with data read-back (needs a cluster with the operator)
	bash tools/e2e/cnpg_pg_upgrade.sh

# The chart on CloudNativePG backed up with the Barman Cloud plugin to an S3 store, removed and
# restored with postgres.cnpg.bootstrap.recovery as docs/deployment.md describes, with data
# read-back and a planted reinstall without the recovery (gap audit C14-04). Needs a cluster with
# the operator, cert-manager and the plugin: CI runs it in the deploy job on kind; on a workstation
# only inside a VM. Not in verify-all.
e2e-cnpg-backup-restore: ## Chart on CloudNativePG: base backup, database lost, restore from the backup with data read-back (needs the operator and the Barman Cloud plugin)
	bash tools/e2e/cnpg_backup_restore.sh

# The chart installed with its defaults and upgraded from the last published chart, each waited
# on until the API and worker are Available and /readyz answers, after a planted install the API
# refuses (#144). Needs a cluster with the CloudNativePG operator, like e2e-cnpg-pg-upgrade.
e2e-chart-install: ## Chart install + upgrade from the last published chart until Ready (needs a cluster with the operator)
	bash tools/e2e/chart_install.sh

# ── web/ (SvelteKit + sveltesentio) ───────────────────────────────────────────
web-verify: ## pnpm lint/typecheck/test(+coverage)/build + OpenAPI type drift
	cd web
	pnpm install --frozen-lockfile
	sh ../tools/ci/clones_check_test.sh
	PRAETORCTL='$(PRAETORCTL)' python3 ../tools/ci/clones_check.py
	pnpm gen:api
	git diff --exit-code -- src/lib/api/schema.d.ts
	sh ../tools/ci/prettier_ignore_test.sh
	pnpm lint
	pnpm typecheck
	pnpm test -- --coverage
	pnpm build
	python3 ../tools/ci/web_strings_scan.py
	python3 ../tools/ci/web_css_classes_check.py

web-gen: ## OpenAPI -> src/lib/api/schema.d.ts
	cd web && pnpm gen:api

web-e2e: ## Playwright + axe-core (zero violations; never suppressed)
	cd web && pnpm test:e2e

web-e2e-api: ## Postgres + api + fakearr + fakeworker + web e2e Playwright axe sweep (needs Docker unless SMOKE_DSN is set)
	mkdir -p $(E2E_BIN)
	go build -o $(E2E_BIN)/snatcharr ./api/cmd/snatcharr
	go build -o $(E2E_BIN)/fakearr ./api/cmd/fakearr
	go build -o $(E2E_BIN)/fakeworker ./api/cmd/fakeworker
	(cd web && pnpm install --frozen-lockfile && pnpm exec playwright install chromium)
	E2E_POSTGRES=$(E2E_POSTGRES) tools/e2e/postgres.sh \
	  env SMOKE_API_BIN=$(E2E_BIN)/snatcharr SMOKE_FAKEARR_BIN=$(E2E_BIN)/fakearr \
	  SMOKE_FAKEWORKER_BIN=$(E2E_BIN)/fakeworker \
	  tools/e2e/web-e2e-api.sh

# Visual baseline of the public pages (/login, /setup) and the signed-in shell (desktop and
# phone; ADR-0016), en and de, light and dark, inside the Playwright image pinned in
# web/e2e/visual-image.env, compared with web/e2e/visual (gap audit C15-03). The planted cases
# shift the layout, recolour the primary button and recolour the shell's current navigation
# entry; the comparison must refuse all three. Not in verify-all (needs Docker).
web-visual: ## Visual baseline of the public pages and the shell in the pinned Playwright image, after three planted diffs (needs Docker)
	bash tools/e2e/planted.sh SNATCHARR_VISUAL_PLANT tools/e2e/web_visual.sh \
	  "shift:of all image pixels) are different" "brand:of all image pixels) are different" \
	  "nav:of all image pixels) are different"

web-visual-update: ## Rewrite the visual baseline in the pinned Playwright image; review the image diff before committing (needs Docker)
	bash tools/e2e/web_visual.sh --update

# ── proto/ ────────────────────────────────────────────────────────────────────
# buf at the tools/go/go.mod pin, run through go tool: no buf on PATH needed, and none is used.
# := so an environment BUF cannot swap in another binary.
BUF := go tool -modfile=$(CURDIR)/tools/go/go.mod buf
proto-verify: ## buf lint + breaking (vs main) + generation drift
	cd proto
	$(BUF) lint
	# Against origin/main, which must exist: without it the check was skipped and passed (A2-05).
	if ! git rev-parse --verify -q origin/main >/dev/null; then
	  echo "proto-verify: no origin/main to check breaking changes against; run git fetch origin main" >&2; exit 1
	fi
	$(BUF) breaking --against '../.git#ref=origin/main,subdir=proto'
	cd ..
	$(MAKE) proto-gen
	git diff --exit-code -- api/internal/gen

proto-gen: ## buf generate (Go stubs; Rust stubs build via tonic-build)
	cd proto && $(BUF) generate
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
chart-package: ## helm lint + package deploy/helm/snatcharr as dist/snatcharr-$(CHART_VERSION).tgz (release-build.yml)
	case "$(CHART_VERSION)" in [0-9]*.[0-9]*.[0-9]*) ;; *) echo "chart-package: set CHART_VERSION=X.Y.Z, got '$(CHART_VERSION)'" >&2; exit 2 ;; esac
	helm lint deploy/helm/snatcharr
	helm package deploy/helm/snatcharr --version "$(CHART_VERSION)" --app-version "$(CHART_VERSION)" -d dist

deploy-verify: ## helm unittest + render.sh + chart_checks.sh planted cases + helm lint --strict + kubeconform (default and all-features renders, oldest claimed and newest Kubernetes) + kustomize build + compose image pins + every object in the release namespace
	if ! helm plugin list 2>/dev/null | grep -E -q '^unittest[[:space:]]'; then \
	  echo "deploy-verify: helm plugin 'unittest' is missing; install helm-unittest (https://github.com/helm-unittest/helm-unittest)" >&2; exit 1; \
	fi
	helm unittest deploy/helm/snatcharr
	sh tools/deploy/render_test.sh
	helm lint --strict deploy/helm/snatcharr
	render=$$(mktemp -d)
	trap 'rm -rf "$$render"' EXIT
	# Rendered to a file first: a pipe into kubeconform passed a failing render (A2-01).
	tools/deploy/render.sh "$$render/default.yaml" snatcharr deploy/helm/snatcharr --namespace snatcharr
	tools/deploy/render.sh "$$render/all.yaml" snatcharr deploy/helm/snatcharr --namespace snatcharr \
	  --set networkPolicy.enabled=true --set api.ingress.enabled=true --set serviceMonitor.enabled=true \
	  --set worker.autoscaling.enabled=true --set api.pdb.enabled=true --set worker.pdb.enabled=true \
	  --set configarr.enabled=true --set configarr.existingConfigMap=cfg --set configarr.existingSecret=sec \
	  --set-json 'networkPolicy.arrPeers=[{"namespaceSelector":{"matchLabels":{"kubernetes.io/metadata.name":"media"}}},{"podSelector":{"matchLabels":{"app.kubernetes.io/name":"lidarr"}},"ports":[{"port":8686,"protocol":"TCP"}]}]'
	# The KEDA ScaledObject excludes the CPU HPA (#43), so it gets its own render.
	tools/deploy/render.sh "$$render/keda.yaml" snatcharr deploy/helm/snatcharr --namespace snatcharr \
	  --set worker.autoscaling.keda.enabled=true --set worker.autoscaling.keda.serverAddress=http://prometheus:9090
	# kubeconform on the kubeVersion minimum of Chart.yaml and on the newest schemas (C13-01),
	# planted cases first. The kubeconform binary if installed, else the pinned image CI uses.
	sh tools/deploy/kubeconform_test.sh
	sh tools/deploy/kubeconform.sh deploy/helm/snatcharr/Chart.yaml "$$render/default.yaml" "$$render/all.yaml" "$$render/keda.yaml"
	kustomize build --enable-helm deploy/kustomize/kind >"$$render/kind.yaml"
	# Every compose image pinned by tag and digest, never latest (#140); planted cases first.
	sh tools/deploy/compose_pins_test.sh
	sh tools/deploy/compose_pins.sh deploy/compose/docker-compose.yml deploy/homelab/compose.yaml
	# Chart checks with their planted cases first (A3-01): no egress rule with an empty `to`
	# (#120), an arrPeers entry with `ports` reachable on those ports only (#145), Chart.yaml
	# release-please markers and image annotation (#122).
	sh tools/deploy/chart_checks_test.sh
	sh tools/deploy/chart_checks.sh netpol "$$render/all.yaml"
	tools/deploy/render.sh "$$render/no-peers.yaml" snatcharr deploy/helm/snatcharr \
	  --set networkPolicy.enabled=true --set-json 'networkPolicy.arrCidrs=[]'
	# Without *arr peers: the API's database and public HTTPS rules and the worker's API rule.
	sh tools/deploy/chart_checks.sh netpol "$$render/no-peers.yaml" "3 with a to, 0 of them without ports, 0 with an empty to"
	# With one arrPeers entry that has `ports` as the only *arr peer, both policies hold two egress
	# rules with a `to` (database or API, and the peer), the API a third for public HTTPS, and each
	# of the five carries `ports`.
	tools/deploy/render.sh "$$render/peer.yaml" snatcharr deploy/helm/snatcharr \
	  --set networkPolicy.enabled=true --set-json 'networkPolicy.arrCidrs=[]' \
	  --set-json 'networkPolicy.arrPeers=[{"podSelector":{"matchLabels":{"app.kubernetes.io/name":"lidarr"}},"ports":[{"port":8686,"protocol":"TCP"}]}]'
	sh tools/deploy/chart_checks.sh netpol "$$render/peer.yaml" "5 with a to, 0 of them without ports, 0 with an empty to"
	app=$$(helm show chart deploy/helm/snatcharr | sed -n 's/^appVersion: *//p' | tr -d "\"'")
	sh tools/deploy/chart_checks.sh chart deploy/helm/snatcharr/Chart.yaml "$$app"
	# Every object in the release namespace, also through the kind overlay: kustomize 5.8 no
	# longer places helmCharts objects that carry no namespace (#346).
	for f in default all keda kind; do sh tools/deploy/chart_checks.sh namespace "$$render/$$f.yaml" snatcharr; done

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

kind-keda-proof: ## KEDA scales the worker on queued runs, in the VM's kind cluster after kind-up (#43)
	KUBECONFIG=$(KIND_KUBECONFIG) tools/e2e/keda_scale.sh

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

# ── Third-party licences ─────────────────────────────────────────────────────
# Shipped dependencies across Go (api), Rust (snatch-worker) and npm (web prod).
# cargo-about 0.9.2 requires the cli feature: cargo install --locked cargo-about --version 0.9.2 --features cli
licences: ## Generate web/static/licences.json for shipped dependencies
	python3 tools/licences/generate.py

licences-check: ## Verify web/static/licences.json is up to date and test planted mutations
	python3 tools/licences/generate.py --check
	bash tools/licences/generate_test.sh

# ── Integrated-app logos ──────────────────────────────────────────────────────
logos: ## Download integrated-app logos to web/static/logos/
	python3 tools/logos/fetch.py

logos-check: ## Verify logos manifest, hashes, and planted mutations
	python3 tools/logos/fetch.py --check
	bash tools/logos/fetch_test.sh

governance-verify: ## compile-context --verify, audit, state audit, flavor audit (go-service)
	$(PRAETORCTL) compile-context --verify
	$(PRAETORCTL) audit
	$(PRAETORCTL) state audit .
	$(PRAETORCTL) flavor audit --flavor=go-service .

hooks-verify: ## The commit-message policy against its accepted and refused cases
	sh .config/hooks/commit-msg_test.sh

secret-scan: ## gitleaks over every local ref, as CI job secret-scan (pinned release, cached in .cache/)
	sh tools/ci/gitleaks_scan.sh .cache/gitleaks

py-lint: ## ruff over every tracked Python file, pinned, config shared with Codacy (ruff.toml), with planted cases
	bash tools/ci/ruff_check_test.sh
	sh tools/ci/ruff_check.sh .cache/ruff

docs-verify: ## mkdocs --strict build with the pinned docs toolchain, with planted cases
	bash tools/ci/docs_check_test.sh .cache/docs
	sh tools/ci/docs_check.sh .cache/docs

release-e2e-check: ## Release feat/fix commits since the previous tag each map to a web/e2e/features test
	python3 -I tools/ci/release_e2e_check.py --self-test
	python3 -I tools/ci/release_e2e_check.py

release-dogfood-check: ## Release dogfood gate and MCP dogfood pass recorder against their planted cases
	python3 -I tools/release/dogfood_gate.py --self-test
	python3 -I tools/release/dogfood_pass.py --self-test

roadmap-check: ## docs/roadmap.md milestone table shape (offline) and the scheduled refresh's bot pull-request step, with planted cases
	python3 -I tools/ci/roadmap_render.py --self-test
	python3 -I tools/ci/roadmap_render.py --check-shape
	sh tools/ci/roadmap_test.sh

workflows-verify: ## The rule of "CI success", the Security Scan issue step, the Semgrep scan, generated_files.py, the planted-case runner, touched-function headers, Codacy exclusions of generated code, inline nosec reasons and the release pull request body, against their cases
	sh tools/ci/status_test.sh
	sh tools/ci/security_issue_test.sh
	sh tools/ci/semgrep_scan_test.sh
	bash tools/ci/generated_files_test.sh
	bash tools/e2e/planted_test.sh
	bash tools/e2e/schema_check_test.sh
	bash tools/ci/func_headers_check_test.sh
	python3 -I tools/ci/func_headers_check.py
	python3 -I tools/ci/codacy_config.py --self-test
	python3 -I tools/ci/codacy_config.py
	bash tools/ci/nosec_check_test.sh
	python3 -I tools/ci/nosec_check.py
	bash tools/ci/release_body_check_test.sh
	bash tools/e2e/live_lib_test.sh
	python3 -I tools/release/dogfood_gate.py --self-test
	python3 -I tools/release/dogfood_pass.py --self-test

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
