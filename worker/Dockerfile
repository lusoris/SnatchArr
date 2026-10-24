# SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
# SPDX-License-Identifier: EUPL-1.2
#
# snatcharr-worker: Rust snatch data-plane. Static musl build via cargo-zigbuild → distroless static.

FROM --platform=$BUILDPLATFORM rust:1-bookworm@sha256:e12566d5abc17a0da741628782cdb504b68b8961f5bfcf677936bf3e8b32de29 AS build
ARG TARGETARCH
RUN apt-get update && apt-get install -y --no-install-recommends xz-utils curl ca-certificates \
 && rm -rf /var/lib/apt/lists/*
# zig for cross-linking musl targets (tarballs are named zig-<arch>-<os>-<version> since 0.14.1).
# The tarball is checked against the SHA-256 of https://ziglang.org/download/index.json before
# it is unpacked; a new ZIG_VERSION needs both checksums from that file.
RUN ZIG_VERSION=0.14.1 && ZIG_ARCH="$(uname -m)" \
 && case "${ZIG_ARCH}" in \
      x86_64) ZIG_SHA256=24aeeec8af16c381934a6cd7d95c807a8cb2cf7df9fa40d359aa884195c4716c ;; \
      aarch64) ZIG_SHA256=f7a654acc967864f7a050ddacfaa778c7504a0eca8d2b678839c21eea47c992b ;; \
      *) echo "no zig checksum for ${ZIG_ARCH}" >&2; exit 1 ;; \
    esac \
 && curl -fsSL -o /tmp/zig.tar.xz "https://ziglang.org/download/${ZIG_VERSION}/zig-${ZIG_ARCH}-linux-${ZIG_VERSION}.tar.xz" \
 && echo "${ZIG_SHA256}  /tmp/zig.tar.xz" | sha256sum -c - \
 && tar -xJf /tmp/zig.tar.xz -C /opt && rm /tmp/zig.tar.xz \
 && ln -s "/opt/zig-${ZIG_ARCH}-linux-${ZIG_VERSION}/zig" /usr/local/bin/zig
WORKDIR /src
# rust-toolchain.toml pins the Rust version and lists the musl targets; `rustup toolchain
# install` installs that toolchain before the sources land so the layer caches.
COPY rust-toolchain.toml ./
RUN rustup toolchain install
RUN cargo install cargo-zigbuild@0.24.0 --locked
COPY proto/ ./proto/
COPY Cargo.toml Cargo.lock ./
COPY worker/ ./worker/
RUN case "${TARGETARCH}" in \
      amd64) T=x86_64-unknown-linux-musl ;; \
      arm64) T=aarch64-unknown-linux-musl ;; \
      *) echo "unsupported TARGETARCH=${TARGETARCH}" >&2; exit 1 ;; \
    esac \
 && cargo zigbuild --release --locked --target "$T" -p snatch-worker \
 && cp "target/$T/release/snatch-worker" /out-snatch-worker

FROM gcr.io/distroless/static-debian13:nonroot@sha256:e2e927ec666bae08560abb3c55d0659eceabb657f56b6782ab500a9fc7f555e3
COPY --from=build /out-snatch-worker /snatch-worker
USER 65532:65532
EXPOSE 8081
ENTRYPOINT ["/snatch-worker"]
