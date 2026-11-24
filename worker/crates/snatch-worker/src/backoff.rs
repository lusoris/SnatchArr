// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

//! Bounded random retry waits so workers refused together do not retry in lockstep.

use std::time::Duration;

use rand::RngExt;

/// A random wait between half of `d` and `d`, so workers refused together do not retry together.
pub fn jitter(d: Duration) -> Duration {
    jitter_with(d, &mut rand::rng())
}

/// A random wait between half of `d` and `d`, drawn from `rng`.
pub fn jitter_with<R: rand::Rng + ?Sized>(d: Duration, rng: &mut R) -> Duration {
    let half = d.checked_div(2).unwrap_or(Duration::ZERO);
    rng.random_range(half..=d)
}

#[cfg(test)]
mod tests {
    use std::collections::BTreeSet;

    use rand::SeedableRng;
    use rand::rngs::StdRng;

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
    fn seeded_jitter_stays_between_half_and_whole() {
        let low = Duration::from_millis(500);
        let high = Duration::from_secs(1);
        for seed in [1, 2, 3] {
            let mut rng = StdRng::seed_from_u64(seed);
            for _ in 0..256 {
                let d = jitter_with(high, &mut rng);
                assert!(
                    (low..=high).contains(&d),
                    "seed {seed}: {d:?} outside {low:?}..={high:?}"
                );
            }
            assert_eq!(jitter_with(Duration::ZERO, &mut rng), Duration::ZERO);
        }
    }

    #[test]
    fn same_seed_gives_same_waits() {
        let mut first_rng = StdRng::seed_from_u64(7);
        let mut second_rng = StdRng::seed_from_u64(7);
        let first: Vec<Duration> = (0..32)
            .map(|_| jitter_with(Duration::from_secs(1), &mut first_rng))
            .collect();
        let second: Vec<Duration> = (0..32)
            .map(|_| jitter_with(Duration::from_secs(1), &mut second_rng))
            .collect();
        assert_eq!(first, second);
    }

    #[test]
    fn different_seeds_give_different_waits() {
        let mut first_rng = StdRng::seed_from_u64(1);
        let mut second_rng = StdRng::seed_from_u64(2);
        let first: Vec<Duration> = (0..32)
            .map(|_| jitter_with(Duration::from_secs(1), &mut first_rng))
            .collect();
        let second: Vec<Duration> = (0..32)
            .map(|_| jitter_with(Duration::from_secs(1), &mut second_rng))
            .collect();
        assert_ne!(first, second);
    }

    #[test]
    fn jitter_does_not_follow_a_schedule() {
        let draws: BTreeSet<Duration> = (0..64).map(|_| jitter(Duration::from_secs(1))).collect();
        let distinct = draws.len();
        assert!(
            distinct >= 60,
            "want >= 60 distinct waits across 64 draws, got {distinct}"
        );
    }
}
