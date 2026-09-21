#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
set -euo pipefail

# Go toolchain
go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2
go install github.com/securego/gosec/v2/cmd/gosec@latest
go install golang.org/x/vuln/cmd/govulncheck@latest
go install github.com/air-verse/air@latest
go install github.com/bufbuild/buf/cmd/buf@latest
go install github.com/evilmartians/lefthook/v2@latest
go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest
go install github.com/ogen-go/ogen/cmd/ogen@latest
go install github.com/cordanaLLM/praetor/cmd/standardsctl@main
ln -sf "$(go env GOPATH)/bin/standardsctl" "$(go env GOPATH)/bin/praetorctl"

# Rust toolchain
rustup component add clippy rustfmt
cargo install cargo-audit cargo-deny cargo-watch

# Node
corepack enable && corepack prepare pnpm@latest --activate

# Kubernetes tooling
curl -fsSL https://raw.githubusercontent.com/helm/helm/main/scripts/get-helm-3 | bash
go install github.com/yannh/kubeconform/cmd/kubeconform@latest
go install sigs.k8s.io/kind@latest

lefthook install
praetorctl state init --if-absent .
