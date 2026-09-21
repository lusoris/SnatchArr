# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# snatcharr-api: Go control plane with the SvelteKit SPA embedded.
# Hermetic multi-stage build → distroless static, non-root, read-only (praetor ADR-0005).

# ── web build ─────────────────────────────────────────────────────────────────
FROM node:24-bookworm-slim AS web
WORKDIR /src/web
RUN corepack enable
COPY web/package.json web/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile
COPY web/ ./
COPY api/openapi/openapi.yaml /src/api/openapi/openapi.yaml
RUN pnpm gen:api && pnpm build

# ── go build ──────────────────────────────────────────────────────────────────
FROM golang:1.27-bookworm AS build
WORKDIR /src/api
COPY api/go.mod api/go.sum ./
RUN go mod download
COPY api/ ./
COPY --from=web /src/web/build ./internal/webui/dist
ARG VERSION=dev
ARG COMMIT=unknown
ARG DATE=unknown
RUN CGO_ENABLED=0 go build -trimpath \
      -ldflags="-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.date=${DATE}" \
      -o /out/snatcharr-api ./cmd/snatcharr

# ── runtime ───────────────────────────────────────────────────────────────────
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/snatcharr-api /snatcharr-api
USER 65532:65532
EXPOSE 8080 9090
ENTRYPOINT ["/snatcharr-api"]
