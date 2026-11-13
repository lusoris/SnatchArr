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
    /// Address of the health listener (`/livez`, `/readyz`, `/metrics`).
    pub http_addr: SocketAddr,
    /// Age of the last successful API call after which `/livez` fails.
    pub stale: Duration,
}

const MIN_CONCURRENCY: usize = 1;
const MAX_CONCURRENCY: usize = 16;
const MIN_HEARTBEAT_SECS: u64 = 5;
const MAX_HEARTBEAT_SECS: u64 = 60;
const MIN_LEASE_WAIT_SECS: u32 = 1;
const MAX_LEASE_WAIT_SECS: u32 = 30;
const MIN_ARR_TIMEOUT_SECS: u64 = 5;
const MAX_ARR_TIMEOUT_SECS: u64 = 120;
const MIN_STALE_SECS: u64 = 60;
const MAX_STALE_SECS: u64 = 3600;

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
    /// A numeric variable was outside its supported range.
    #[error("config: {name}={value} is outside allowed range {min}..={max}")]
    OutOfRange {
        /// Variable name.
        name: &'static str,
        /// Checked value.
        value: u64,
        /// Smallest accepted value.
        min: u64,
        /// Largest accepted value.
        max: u64,
    },
    /// The stale limit leaves no room for the calls that refresh it.
    #[error(
        "config: SNATCH_STALE_SECS={stale} must be at least {min}: twice the longer of the heartbeat interval and the lease wait"
    )]
    StaleTooShort {
        /// Stale limit in seconds, after range validation.
        stale: u64,
        /// Smallest accepted limit in seconds.
        min: u64,
    },
}

fn env(get: &impl Fn(&str) -> Option<String>, name: &'static str) -> Option<String> {
    get(name).filter(|v| !v.trim().is_empty())
}

fn worker_id(get: &impl Fn(&str) -> Option<String>) -> String {
    env(get, "SNATCH_WORKER_ID")
        .or_else(|| env(get, "HOSTNAME"))
        .unwrap_or_else(|| format!("worker-{}", std::process::id()))
}

fn env_num<T: std::str::FromStr>(
    get: &impl Fn(&str) -> Option<String>,
    name: &'static str,
    default: T,
    kind: &'static str,
) -> Result<T, ConfigError> {
    match env(get, name) {
        None => Ok(default),
        Some(v) => v.trim().parse().map_err(|_| ConfigError::Invalid {
            name,
            value: v,
            kind,
        }),
    }
}

fn checked_range(value: u64, name: &'static str, min: u64, max: u64) -> Result<u64, ConfigError> {
    if !(min..=max).contains(&value) {
        return Err(ConfigError::OutOfRange {
            name,
            value,
            min,
            max,
        });
    }
    Ok(value)
}

fn checked_range_u32(
    value: u32,
    name: &'static str,
    min: u32,
    max: u32,
) -> Result<u32, ConfigError> {
    if !(min..=max).contains(&value) {
        return Err(ConfigError::OutOfRange {
            name,
            value: u64::from(value),
            min: u64::from(min),
            max: u64::from(max),
        });
    }
    Ok(value)
}

fn checked_range_usize(
    value: usize,
    name: &'static str,
    min: usize,
    max: usize,
) -> Result<usize, ConfigError> {
    if !(min..=max).contains(&value) {
        return Err(ConfigError::OutOfRange {
            name,
            value: u64::try_from(value).unwrap_or(u64::MAX),
            min: u64::try_from(min).unwrap_or(u64::MAX),
            max: u64::try_from(max).unwrap_or(u64::MAX),
        });
    }
    Ok(value)
}

impl Config {
    /// Reads `SNATCH_*` variables.
    pub fn from_env() -> Result<Self, ConfigError> {
        Self::from_lookup(|name| std::env::var(name).ok())
    }

    fn from_lookup(get: impl Fn(&str) -> Option<String>) -> Result<Self, ConfigError> {
        let worker_id = worker_id(&get);
        let heartbeat_secs: u64 = env_num(&get, "SNATCH_HEARTBEAT_SECS", 30, "integer")?;
        let arr_timeout_secs: u64 = env_num(&get, "SNATCH_ARR_TIMEOUT_SECS", 30, "integer")?;
        let lease_wait_secs = checked_range_u32(
            env_num::<u32>(
                &get,
                "SNATCH_LEASE_WAIT_SECS",
                MAX_LEASE_WAIT_SECS,
                "integer",
            )?,
            "SNATCH_LEASE_WAIT_SECS",
            MIN_LEASE_WAIT_SECS,
            MAX_LEASE_WAIT_SECS,
        )?;
        let heartbeat_secs = checked_range(
            heartbeat_secs,
            "SNATCH_HEARTBEAT_SECS",
            MIN_HEARTBEAT_SECS,
            MAX_HEARTBEAT_SECS,
        )?;
        let heartbeat = Duration::from_secs(heartbeat_secs);
        let stale_secs = checked_range(
            env_num(&get, "SNATCH_STALE_SECS", 180, "integer")?,
            "SNATCH_STALE_SECS",
            MIN_STALE_SECS,
            MAX_STALE_SECS,
        )?;
        let concurrency = checked_range_usize(
            env_num::<usize>(&get, "SNATCH_CONCURRENCY", 2, "integer")?,
            "SNATCH_CONCURRENCY",
            MIN_CONCURRENCY,
            MAX_CONCURRENCY,
        )?;
        let arr_timeout_secs = checked_range(
            arr_timeout_secs,
            "SNATCH_ARR_TIMEOUT_SECS",
            MIN_ARR_TIMEOUT_SECS,
            MAX_ARR_TIMEOUT_SECS,
        )?;
        let stale = stale_limit(stale_secs, heartbeat, lease_wait_secs)?;
        Ok(Self {
            api_grpc: env(&get, "SNATCH_API_GRPC")
                .unwrap_or_else(|| "http://127.0.0.1:9090".to_owned()),
            token: env(&get, "SNATCH_WORKER_TOKEN"),
            worker_id,
            concurrency,
            lease_wait_secs,
            heartbeat,
            arr_timeout: Duration::from_secs(arr_timeout_secs),
            http_addr: env_num(
                &get,
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
    use std::collections::HashMap;
    use std::fs;
    use std::path::Path;

    fn env_lookup(pairs: &[(&str, &str)]) -> impl Fn(&str) -> Option<String> {
        let map: HashMap<String, String> = pairs
            .iter()
            .map(|(k, v)| ((*k).to_owned(), (*v).to_owned()))
            .collect();
        move |name| map.get(name).cloned()
    }

    fn from_vars(pairs: &[(&str, &str)]) -> Result<Config, ConfigError> {
        Config::from_lookup(env_lookup(pairs))
    }

    fn from_numeric_var(name: &'static str, value: u64) -> Result<Config, ConfigError> {
        let value = value.to_string();
        Config::from_lookup(|requested| (requested == name).then(|| value.clone()))
    }

    #[test]
    fn defaults_are_bounded() {
        let c = Config::from_lookup(|_| None).expect("defaults");
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

    #[test]
    fn positive_all_variables_parsed() {
        let vars = [
            ("SNATCH_API_GRPC", "http://10.0.0.1:9090"),
            ("SNATCH_WORKER_TOKEN", "bearer-token-xyz"),
            ("SNATCH_WORKER_ID", "worker-custom-1"),
            ("SNATCH_CONCURRENCY", "8"),
            ("SNATCH_HEARTBEAT_SECS", "45"),
            ("SNATCH_LEASE_WAIT_SECS", "20"),
            ("SNATCH_ARR_TIMEOUT_SECS", "60"),
            ("SNATCH_STALE_SECS", "240"),
            ("SNATCH_HTTP_ADDR", "127.0.0.1:9999"),
        ];
        let c = from_vars(&vars).expect("valid config");
        assert_eq!(c.api_grpc, "http://10.0.0.1:9090");
        assert_eq!(c.token.as_deref(), Some("bearer-token-xyz"));
        assert_eq!(c.worker_id, "worker-custom-1");
        assert_eq!(c.concurrency, 8);
        assert_eq!(c.heartbeat, Duration::from_secs(45));
        assert_eq!(c.lease_wait_secs, 20);
        assert_eq!(c.arr_timeout, Duration::from_secs(60));
        assert_eq!(c.stale, Duration::from_secs(240));
        assert_eq!(c.http_addr, "127.0.0.1:9999".parse().expect("addr"));
    }

    #[test]
    fn negative_non_numeric_integers() {
        let vars = [
            "SNATCH_CONCURRENCY",
            "SNATCH_HEARTBEAT_SECS",
            "SNATCH_LEASE_WAIT_SECS",
            "SNATCH_ARR_TIMEOUT_SECS",
            "SNATCH_STALE_SECS",
        ];
        for var in vars {
            let err = from_vars(&[(var, "not_a_number")]).expect_err(var);
            assert!(matches!(
                err,
                ConfigError::Invalid { name, value, kind }
                    if name == var && value == "not_a_number" && kind == "integer"
            ));
        }
    }

    #[test]
    fn negative_negative_numbers() {
        let vars = [
            "SNATCH_CONCURRENCY",
            "SNATCH_HEARTBEAT_SECS",
            "SNATCH_LEASE_WAIT_SECS",
            "SNATCH_ARR_TIMEOUT_SECS",
            "SNATCH_STALE_SECS",
        ];
        for var in vars {
            let err = from_vars(&[(var, "-1")]).expect_err(var);
            assert!(matches!(
                err,
                ConfigError::Invalid { name, value, kind }
                    if name == var && value == "-1" && kind == "integer"
            ));
        }
    }

    #[test]
    fn negative_invalid_socket_address() {
        let err = from_vars(&[("SNATCH_HTTP_ADDR", "not-an-ip-or-port")]).expect_err("socket addr");
        assert!(matches!(
            err,
            ConfigError::Invalid { name, value, kind }
                if name == "SNATCH_HTTP_ADDR" && value == "not-an-ip-or-port" && kind == "socket address"
        ));
    }

    #[test]
    fn numeric_ranges_accept_boundaries_and_reject_outside_values() {
        let cases = [
            (
                "SNATCH_CONCURRENCY",
                u64::try_from(MIN_CONCURRENCY).expect("concurrency minimum fits u64"),
                u64::try_from(MAX_CONCURRENCY).expect("concurrency maximum fits u64"),
            ),
            (
                "SNATCH_HEARTBEAT_SECS",
                MIN_HEARTBEAT_SECS,
                MAX_HEARTBEAT_SECS,
            ),
            (
                "SNATCH_LEASE_WAIT_SECS",
                u64::from(MIN_LEASE_WAIT_SECS),
                u64::from(MAX_LEASE_WAIT_SECS),
            ),
            (
                "SNATCH_ARR_TIMEOUT_SECS",
                MIN_ARR_TIMEOUT_SECS,
                MAX_ARR_TIMEOUT_SECS,
            ),
            ("SNATCH_STALE_SECS", MIN_STALE_SECS, MAX_STALE_SECS),
        ];
        for (name, min, max) in cases {
            assert!(from_numeric_var(name, min).is_ok(), "{name} minimum");
            assert!(from_numeric_var(name, max).is_ok(), "{name} maximum");

            for value in [min - 1, max + 1] {
                let err = from_numeric_var(name, value).expect_err("out-of-range value");
                assert!(matches!(
                    err,
                    ConfigError::OutOfRange {
                        name: actual_name,
                        value: actual_value,
                        min: actual_min,
                        max: actual_max,
                    } if actual_name == name
                        && actual_value == value
                        && actual_min == min
                        && actual_max == max
                ));
            }
        }
    }

    #[test]
    fn helm_worker_ranges_match_worker_ranges() {
        let schema_path = Path::new(env!("CARGO_MANIFEST_DIR"))
            .join("../../../deploy/helm/snatcharr/values.schema.json");
        let schema: serde_json::Value = serde_json::from_str(
            &fs::read_to_string(&schema_path).expect("read Helm values schema"),
        )
        .expect("parse Helm values schema");
        let cases = [
            (
                "concurrency",
                u64::try_from(MIN_CONCURRENCY).expect("concurrency minimum fits u64"),
                u64::try_from(MAX_CONCURRENCY).expect("concurrency maximum fits u64"),
            ),
            ("heartbeatSecs", MIN_HEARTBEAT_SECS, MAX_HEARTBEAT_SECS),
            (
                "leaseWaitSecs",
                u64::from(MIN_LEASE_WAIT_SECS),
                u64::from(MAX_LEASE_WAIT_SECS),
            ),
            ("arrTimeoutSecs", MIN_ARR_TIMEOUT_SECS, MAX_ARR_TIMEOUT_SECS),
            ("staleSecs", MIN_STALE_SECS, MAX_STALE_SECS),
        ];
        for (field, min, max) in cases {
            let property = schema
                .get("properties")
                .and_then(serde_json::Value::as_object)
                .and_then(|properties| properties.get("worker"))
                .and_then(serde_json::Value::as_object)
                .and_then(|worker| worker.get("allOf"))
                .and_then(serde_json::Value::as_array)
                .and_then(|all_of| all_of.get(1))
                .and_then(serde_json::Value::as_object)
                .and_then(|schema| schema.get("properties"))
                .and_then(serde_json::Value::as_object)
                .and_then(|properties| properties.get(field))
                .expect("worker field in Helm values schema");
            assert_eq!(property["minimum"].as_u64(), Some(min), "{field} minimum");
            assert_eq!(property["maximum"].as_u64(), Some(max), "{field} maximum");
        }
    }

    #[test]
    fn stale_too_short_boundary() {
        let err = from_vars(&[
            ("SNATCH_HEARTBEAT_SECS", "40"),
            ("SNATCH_LEASE_WAIT_SECS", "20"),
            ("SNATCH_STALE_SECS", "79"),
        ])
        .expect_err("stale 79 < min 80");
        assert!(matches!(
            err,
            ConfigError::StaleTooShort { stale: 79, min: 80 }
        ));

        let c = from_vars(&[
            ("SNATCH_HEARTBEAT_SECS", "40"),
            ("SNATCH_LEASE_WAIT_SECS", "20"),
            ("SNATCH_STALE_SECS", "80"),
        ])
        .expect("stale 80 == min 80");
        assert_eq!(c.stale, Duration::from_secs(80));
    }

    #[test]
    fn blank_values_treated_as_unset() {
        let c = from_vars(&[
            ("SNATCH_API_GRPC", "   "),
            ("SNATCH_WORKER_TOKEN", " \t\n "),
            ("SNATCH_CONCURRENCY", "  "),
            ("SNATCH_HEARTBEAT_SECS", " \t "),
            ("SNATCH_LEASE_WAIT_SECS", " "),
            ("SNATCH_ARR_TIMEOUT_SECS", "   "),
            ("SNATCH_STALE_SECS", "  "),
            ("SNATCH_HTTP_ADDR", "   "),
        ])
        .expect("blank values treated as unset");
        assert_eq!(c.api_grpc, "http://127.0.0.1:9090");
        assert_eq!(c.token, None);
        assert_eq!(c.concurrency, 2);
        assert_eq!(c.heartbeat, Duration::from_secs(30));
        assert_eq!(c.lease_wait_secs, 30);
        assert_eq!(c.arr_timeout, Duration::from_secs(30));
        assert_eq!(c.stale, Duration::from_secs(180));
        assert_eq!(c.http_addr, SocketAddr::from(([0, 0, 0, 0], 8081)));
    }

    #[test]
    fn padded_numbers_are_trimmed() {
        let c = from_vars(&[
            ("SNATCH_CONCURRENCY", " 8 "),
            ("SNATCH_HTTP_ADDR", " 127.0.0.1:9999 "),
        ])
        .expect("padded values parse");
        assert_eq!(c.concurrency, 8);
        assert_eq!(c.http_addr.port(), 9999);
    }

    #[test]
    fn worker_id_precedence() {
        let c1 = from_vars(&[("SNATCH_WORKER_ID", "id-wins"), ("HOSTNAME", "host-loses")])
            .expect("worker_id set");
        assert_eq!(c1.worker_id, "id-wins");

        let c2 = from_vars(&[("HOSTNAME", "host-used")]).expect("hostname used");
        assert_eq!(c2.worker_id, "host-used");

        let c3 = from_vars(&[
            ("SNATCH_WORKER_ID", "   "),
            ("HOSTNAME", "host-used-blank-id"),
        ])
        .expect("hostname used when id blank");
        assert_eq!(c3.worker_id, "host-used-blank-id");

        let c4 = from_vars(&[]).expect("fallback worker id");
        assert_eq!(c4.worker_id, format!("worker-{}", std::process::id()));
    }

    #[test]
    fn token_presence_and_absence() {
        let unset = from_vars(&[]).expect("token unset");
        assert_eq!(unset.token, None);

        let set = from_vars(&[("SNATCH_WORKER_TOKEN", "token-secret")]).expect("token set");
        assert_eq!(set.token.as_deref(), Some("token-secret"));

        let blank = from_vars(&[("SNATCH_WORKER_TOKEN", "   ")]).expect("token blank");
        assert_eq!(blank.token, None);
    }
}
