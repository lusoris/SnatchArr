// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

//! Bearer-token interceptor for every RPC to the API.

use tonic::metadata::MetadataValue;
use tonic::service::Interceptor;
use tonic::{Request, Status};

/// Adds `authorization: Bearer <token>` when a token is configured.
#[derive(Clone, Debug)]
pub struct AuthInterceptor {
    header: Option<MetadataValue<tonic::metadata::Ascii>>,
}

impl AuthInterceptor {
    /// Builds the interceptor; an unparsable token is rejected up front.
    pub fn new(token: Option<&str>) -> Result<Self, tonic::metadata::errors::InvalidMetadataValue> {
        let header = match token {
            Some(t) => Some(format!("Bearer {t}").parse()?),
            None => None,
        };
        Ok(Self { header })
    }
}

impl Interceptor for AuthInterceptor {
    fn call(&mut self, mut req: Request<()>) -> Result<Request<()>, Status> {
        if let Some(h) = &self.header {
            req.metadata_mut().insert("authorization", h.clone());
        }
        Ok(req)
    }
}
