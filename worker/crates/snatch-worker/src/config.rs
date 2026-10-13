// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

//! Environment configuration. Every knob has a bounded default.

use std::net::SocketAddr;
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
    /// Address of the health listener (`/livez`, `/metrics`).
    pub http_addr: SocketAddr,
    /// Age of the last successful API call after which `/livez` fails.
    pub stale: Duration,
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
    /// The stale limit leaves no room for the calls that refresh it.
    #[error(
        "config: SNATCH_STALE_SECS={stale} must be at least {min}: twice the longer of the heartbeat interval and the lease wait"
    )]
    StaleTooShort {
        /// Stale limit in seconds, after clamping.
        stale: u64,
        /// Smallest accepted limit in seconds.
        min: u64,
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
        let lease_wait_secs = env_num::<u32>("SNATCH_LEASE_WAIT_SECS", 30, "integer")?.clamp(1, 30);
        let heartbeat = Duration::from_secs(heartbeat_secs.clamp(5, 60));
        let stale_secs = env_num::<u64>("SNATCH_STALE_SECS", 180, "integer")?.clamp(60, 3600);
        let stale = stale_limit(stale_secs, heartbeat, lease_wait_secs)?;
        Ok(Self {
            api_grpc: env("SNATCH_API_GRPC").unwrap_or_else(|| "http://127.0.0.1:9090".to_owned()),
            token: env("SNATCH_WORKER_TOKEN"),
            worker_id,
            concurrency: env_num::<usize>("SNATCH_CONCURRENCY", 2, "integer")?.clamp(1, 16),
            lease_wait_secs,
            heartbeat,
            arr_timeout: Duration::from_secs(arr_timeout_secs.clamp(5, 120)),
            http_addr: env_num(
                "SNATCH_HTTP_ADDR",
                SocketAddr::from(([0, 0, 0, 0], 8081)),
                "socket address",
            )?,
            stale,
        })
    }
}

/// The stale limit, refused when a healthy worker could miss it: an idle worker calls LeaseRun
/// at least every lease wait, a busy one heartbeats every heartbeat interval.
fn stale_limit(
    stale_secs: u64,
    heartbeat: Duration,
    lease_wait_secs: u32,
) -> Result<Duration, ConfigError> {
    let min = heartbeat
        .as_secs()
        .max(u64::from(lease_wait_secs))
        .saturating_mul(2);
    if stale_secs < min {
        return Err(ConfigError::StaleTooShort {
            stale: stale_secs,
            min,
        });
    }
    Ok(Duration::from_secs(stale_secs))
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
        assert_eq!(c.stale, Duration::from_secs(180));
        assert_eq!(c.http_addr.port(), 8081);
    }

    #[test]
    fn stale_limit_needs_twice_the_longest_interval() {
        let hb = Duration::from_secs(60);
        assert_eq!(
            stale_limit(120, hb, 30).ok(),
            Some(Duration::from_secs(120))
        );
        assert!(matches!(
            stale_limit(119, hb, 30),
            Err(ConfigError::StaleTooShort {
                stale: 119,
                min: 120
            })
        ));
        // The lease wait counts when it is the longer interval.
        assert!(matches!(
            stale_limit(59, Duration::from_secs(5), 30),
            Err(ConfigError::StaleTooShort { min: 60, .. })
        ));
        assert!(stale_limit(60, Duration::from_secs(5), 30).is_ok());
    }
}
