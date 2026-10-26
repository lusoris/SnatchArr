# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# snatcharr-api: Go control plane with the SvelteKit SPA embedded.
# Hermetic multi-stage build → distroless static, non-root, read-only (praetor ADR-0005).

# ── web build ─────────────────────────────────────────────────────────────────
FROM --platform=$BUILDPLATFORM node:24-bookworm-slim@sha256:55fb5f5a527ea2c5c4a95f963e48ef90aa38c7b346054fdc8ee9957a2b55d205 AS web
WORKDIR /src/web
RUN corepack enable
COPY web/package.json web/pnpm-lock.yaml web/pnpm-workspace.yaml ./
RUN pnpm install --frozen-lockfile
COPY web/ ./
COPY api/openapi/openapi.yaml /src/api/openapi/openapi.yaml
RUN pnpm gen:api && pnpm build

# ── go build ──────────────────────────────────────────────────────────────────
FROM --platform=$BUILDPLATFORM golang:1.27-bookworm@sha256:972b4110ba0f47539a975c31e52c64765e1ff19f2d74469ae4a595c6676e80a5 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY api/ ./api/
COPY --from=web /src/web/build ./api/internal/webui/dist
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
ARG COMMIT=unknown
ARG DATE=unknown
# nodynamic: gen2brain/webp otherwise loads libwebp through purego, which links the
# binary against the glibc loader; distroless/static has none. The readelf step fails
# the build when any program interpreter is present.
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -tags nodynamic \
      -ldflags="-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.date=${DATE}" \
      -o /out/snatcharr-api ./api/cmd/snatcharr \
    && if readelf -l /out/snatcharr-api | grep -q INTERP; then \
         echo "snatcharr-api is dynamically linked" >&2; exit 1; fi

# ── runtime ───────────────────────────────────────────────────────────────────
FROM gcr.io/distroless/static-debian13:nonroot@sha256:e2e927ec666bae08560abb3c55d0659eceabb657f56b6782ab500a9fc7f555e3
COPY --from=build /out/snatcharr-api /snatcharr-api
USER 65532:65532
EXPOSE 8080 9090
ENTRYPOINT ["/snatcharr-api"]
