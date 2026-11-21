# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# snatcharr-api: Go control plane with the SvelteKit SPA embedded.
# Hermetic multi-stage build → distroless static, non-root, read-only (praetor ADR-0005).
# idmatchparity target: docker build --target idmatchparity .

# ── web build ─────────────────────────────────────────────────────────────────
FROM --platform=$BUILDPLATFORM node:24-bookworm-slim@sha256:dc26ae92c9fb15cf6b1d63e20f0944b2efe0f115b20e00be48fb23fceb061c54 AS web
WORKDIR /src/web
COPY web/package.json web/pnpm-lock.yaml web/pnpm-workspace.yaml ./
# Node 25+ ships no corepack; install pnpm from npm at the packageManager version.
RUN pm=$(node -p 'require("./package.json").packageManager') \
    && npm install --global --no-audit --no-fund --loglevel=error "${pm%%+*}"
# patchedDependencies in pnpm-workspace.yaml points here; install fails without it.
COPY web/patches/ ./patches/
RUN pnpm install --frozen-lockfile
COPY web/ ./
COPY api/openapi/openapi.yaml /src/api/openapi/openapi.yaml
RUN pnpm gen:api && pnpm build

# ── go build ──────────────────────────────────────────────────────────────────
FROM --platform=$BUILDPLATFORM golang:1.27-bookworm@sha256:4fa7c968ba93f1635670522dbb01a2c78150443e1385746924566079f36570ea AS build
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

# ── idmatchparity build ───────────────────────────────────────────────────────
FROM --platform=$BUILDPLATFORM golang:1.27-bookworm@sha256:4fa7c968ba93f1635670522dbb01a2c78150443e1385746924566079f36570ea AS idmatchparity-build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY tools/idmatchparity/ ./tools/idmatchparity/
ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -p 4 -trimpath -ldflags="-s -w" \
      -o /out/idmatchparity ./tools/idmatchparity

# ── idmatchparity runtime ─────────────────────────────────────────────────────
FROM gcr.io/distroless/static-debian13:nonroot@sha256:aafe993665bf6200db38a02c11a9351e782a84b6a45e7f33b6695638c88e3931 AS idmatchparity
COPY --from=idmatchparity-build /out/idmatchparity /idmatchparity
USER 65532:65532
ENTRYPOINT ["/idmatchparity"]

# ── runtime ───────────────────────────────────────────────────────────────────
FROM gcr.io/distroless/static-debian13:nonroot@sha256:aafe993665bf6200db38a02c11a9351e782a84b6a45e7f33b6695638c88e3931
COPY --from=build /out/snatcharr-api /snatcharr-api
USER 65532:65532
EXPOSE 8080 9090
ENTRYPOINT ["/snatcharr-api"]
