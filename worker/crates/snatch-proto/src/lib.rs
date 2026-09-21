// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

//! Generated bindings for `snatcharr.v1.WorkerService` (see proto/snatcharr/v1/worker.proto).
//! The Go control plane serves it; this crate only holds the client and messages.

#![allow(missing_docs, clippy::all, clippy::pedantic, clippy::nursery)]

/// `snatcharr.v1` package.
pub mod v1 {
    tonic::include_proto!("snatcharr.v1");
}

pub use v1::*;
