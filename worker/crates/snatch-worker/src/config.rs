// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

//! Environment configuration. Every knob has a bounded default.

use std::time::Duration;

/// Worker settings.
#[derive(Debug, Clone)]
pub struct Config {
    /// gRPC endpoint of the SnatchArr API, e.g. `http://api:9090`.
    pub api_grpc: String,
    /// Shared bearer token (`APP_SNATCHARR_WORKER_TOKEN` on the API side).
    pub token: Option<String>,
    /// Stable identity used in leases.
    pub worker_id: String,
    /// Runs executed concurrently.
    pub concurrency: usize,
    /// Long-poll wait per lease request (seconds, <= 30).
    pub lease_wait_secs: u32,
    /// Heartbeat interval.
    pub heartbeat: Duration,
    /// Per-request *arr timeout.
    pub arr_timeout: Duration,
}

/// Configuration errors.
#[derive(Debug, thiserror::Error)]
pub enum ConfigError {
    /// A numeric variable did not parse.
    #[error("config: {name}={value:?} is not a valid {kind}")]
    Invalid {
        /// Variable name.
        name: &'static str,
        /// Raw value.
        value: String,
        /// Expected kind.
        kind: &'static str,
    },
}

fn env(name: &'static str) -> Option<String> {
    std::env::var(name).ok().filter(|v| !v.trim().is_empty())
}

fn env_num<T: std::str::FromStr>(
    name: &'static str,
    default: T,
    kind: &'static str,
) -> Result<T, ConfigError> {
    match env(name) {
        None => Ok(default),
        Some(v) => v.trim().parse().map_err(|_| ConfigError::Invalid {
            name,
            value: v,
            kind,
        }),
    }
}

impl Config {
    /// Reads `SNATCH_*` variables.
    pub fn from_env() -> Result<Self, ConfigError> {
        let worker_id = env("SNATCH_WORKER_ID")
            .or_else(|| env("HOSTNAME"))
            .unwrap_or_else(|| format!("worker-{}", std::process::id()));
        let heartbeat_secs: u64 = env_num("SNATCH_HEARTBEAT_SECS", 30, "integer")?;
        let arr_timeout_secs: u64 = env_num("SNATCH_ARR_TIMEOUT_SECS", 30, "integer")?;
        Ok(Self {
            api_grpc: env("SNATCH_API_GRPC").unwrap_or_else(|| "http://127.0.0.1:9090".to_owned()),
            token: env("SNATCH_WORKER_TOKEN"),
            worker_id,
            concurrency: env_num::<usize>("SNATCH_CONCURRENCY", 2, "integer")?.clamp(1, 16),
            lease_wait_secs: env_num::<u32>("SNATCH_LEASE_WAIT_SECS", 30, "integer")?.clamp(1, 30),
            heartbeat: Duration::from_secs(heartbeat_secs.clamp(5, 60)),
            arr_timeout: Duration::from_secs(arr_timeout_secs.clamp(5, 120)),
        })
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn defaults_are_bounded() {
        let c = Config::from_env().expect("defaults");
        assert!((1..=16).contains(&c.concurrency));
        assert!((1..=30).contains(&c.lease_wait_secs));
        assert!(c.heartbeat >= Duration::from_secs(5));
    }
}
