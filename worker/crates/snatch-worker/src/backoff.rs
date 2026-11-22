// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

//! Bounded random retry waits so workers refused together do not retry in lockstep.

use std::time::Duration;

use rand::RngExt;

/// A random wait between half of `d` and `d`, so workers refused together do not retry together.
pub fn jitter(d: Duration) -> Duration {
    let half = d.checked_div(2).unwrap_or(Duration::ZERO);
    rand::rng().random_range(half..=d)
}

#[cfg(test)]
mod tests {
    use std::collections::BTreeSet;

    use super::*;

    #[test]
    fn jitter_of_zero_is_zero() {
        assert_eq!(jitter(Duration::ZERO), Duration::ZERO);
    }

    #[test]
    fn jitter_stays_between_half_and_whole() {
        let low = Duration::from_millis(100);
        let high = Duration::from_millis(200);
        for _ in 0..64 {
            let d = jitter(high);
            assert!(
                (low..=high).contains(&d),
                "{d:?} outside {low:?}..={high:?}"
            );
        }
    }

    #[test]
    fn jitter_is_not_constant() {
        let draws: BTreeSet<Duration> = (0..64).map(|_| jitter(Duration::from_secs(1))).collect();
        assert!(draws.len() >= 2, "want >= 2 distinct waits, got {draws:?}");
    }
}
