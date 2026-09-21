# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# snatcharr-worker: Rust snatch data-plane. Static musl build via cargo-zigbuild → distroless static.

FROM rust:1-bookworm AS build
ARG TARGETARCH
RUN apt-get update && apt-get install -y --no-install-recommends xz-utils curl ca-certificates \
 && rm -rf /var/lib/apt/lists/*
# zig for cross-linking musl targets
RUN ZIG_VERSION=0.14.1 && ZIG_ARCH="$(uname -m)" \
 && curl -fsSL "https://ziglang.org/download/${ZIG_VERSION}/zig-linux-${ZIG_ARCH}-${ZIG_VERSION}.tar.xz" | tar -xJ -C /opt \
 && ln -s "/opt/zig-linux-${ZIG_ARCH}-${ZIG_VERSION}/zig" /usr/local/bin/zig
RUN cargo install cargo-zigbuild --locked
RUN rustup target add x86_64-unknown-linux-musl aarch64-unknown-linux-musl
WORKDIR /src
COPY proto/ /proto/
COPY worker/ ./
RUN case "${TARGETARCH}" in \
      amd64) T=x86_64-unknown-linux-musl ;; \
      arm64) T=aarch64-unknown-linux-musl ;; \
      *) echo "unsupported TARGETARCH=${TARGETARCH}" >&2; exit 1 ;; \
    esac \
 && cargo zigbuild --release --locked --target "$T" -p snatch-worker \
 && cp "target/$T/release/snatch-worker" /out-snatch-worker

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out-snatch-worker /snatch-worker
USER 65532:65532
EXPOSE 9091
ENTRYPOINT ["/snatch-worker"]
