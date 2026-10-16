#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
set -euo pipefail

# Go toolchain. Every tool is pinned; Renovate moves the pins (renovate.json, devcontainer tools).
go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0
# gosec and govulncheck at the tools/go/go.mod pins: gosec@latest resolves an x/tools that
# cannot load Go 1.27.2 packages.
make gosec-install govulncheck-install
go install github.com/air-verse/air@v1.67.4
go install github.com/bufbuild/buf/cmd/buf@v1.74.0
go install github.com/evilmartians/lefthook/v2@v2.2.2
go install github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1
# ogen at the go.mod version: inside the module, go install without @version resolves it there.
go install github.com/ogen-go/ogen/cmd/ogen
# The engine pin of CI (PRAETOR_REF in .github/workflows/ci.yml); move both together.
go install github.com/cordanaLLM/praetor/cmd/standardsctl@7057d9957b3a7b98d60899aad55bbee406e0e96c
ln -sf "$(go env GOPATH)/bin/standardsctl" "$(go env GOPATH)/bin/praetorctl"

# Rust toolchain
rustup component add clippy rustfmt
cargo install --locked cargo-audit@0.22.2
cargo install --locked cargo-deny@0.20.2
cargo install --locked cargo-watch@8.5.3

# Node
# pnpm at the packageManager version of web/package.json.
corepack enable && (cd web && corepack install)

# Kubernetes tooling (kubectl and helm come from the kubectl-helm-minikube feature in devcontainer.json)
go install github.com/yannh/kubeconform/cmd/kubeconform@v0.8.0
go install sigs.k8s.io/kind@v0.33.0

lefthook install
praetorctl state init --if-absent .
