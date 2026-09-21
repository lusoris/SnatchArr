// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

//! Compiles ../../../proto/snatcharr/v1/worker.proto with protox (pure Rust, no protoc)
//! and generates the tonic client + prost messages into OUT_DIR.

use std::path::PathBuf;

fn main() -> Result<(), Box<dyn std::error::Error>> {
    let manifest_dir = PathBuf::from(std::env::var("CARGO_MANIFEST_DIR")?);
    let proto_root = manifest_dir.join("../../../proto");
    let proto = proto_root.join("snatcharr/v1/worker.proto");
    println!("cargo:rerun-if-changed={}", proto.display());

    let fds = protox::compile([proto.as_path()], [proto_root.as_path()])?;
    tonic_prost_build::configure()
        .build_server(false)
        .build_client(true)
        .compile_fds(fds)?;
    Ok(())
}
