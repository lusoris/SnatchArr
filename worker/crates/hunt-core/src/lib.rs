// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

//! Pure hunt policy. No I/O, no clocks, no randomness of its own: callers pass `now` and an
//! `Rng`, which keeps every function deterministic under test and property-checkable.
//!
//! One cycle, in order: [`PagePlan::plan`] decides which wanted-list pages to fetch,
//! [`filter`] drops unmonitored and unreleased items, the API removes processed ids,
//! [`select`] picks the batch, a `group_*` function turns items into search targets, and
//! [`cap`] trims the batch to the hourly budget the API granted.

#![cfg_attr(
    test,
    allow(
        clippy::unwrap_used,
        clippy::expect_used,
        clippy::indexing_slicing,
        clippy::cast_possible_truncation,
        clippy::cast_sign_loss,
        clippy::arithmetic_side_effects
    )
)]

use std::collections::BTreeMap;

use rand::RngExt;

/// How candidates are picked from a wanted list.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Selection {
    /// Random pages and random items: spreads searches across the library.
    Random,
    /// Walk the list in order from a cursor.
    Sequential,
}

/// Which pages of a paged wanted list to fetch this cycle.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PagePlan {
    /// 1-based page numbers, distinct, in fetch order.
    pub pages: Vec<u32>,
    /// Cursor to persist for the next sequential cycle (ignored for random).
    pub next_cursor: u32,
}

/// Upper bound on pages fetched per cycle regardless of policy (HISS-02 loop bound).
pub const MAX_PAGES_PER_CYCLE: u32 = 8;

impl PagePlan {
    /// Plans which pages to fetch so that roughly `want * 2` candidates are seen before
    /// filtering, without ever reading the whole list.
    #[must_use]
    pub fn plan<R: RngExt>(
        total_records: u32,
        page_size: u32,
        want: u32,
        selection: Selection,
        cursor: u32,
        rng: &mut R,
    ) -> Self {
        let page_size = page_size.max(1);
        let total_pages = total_records.div_ceil(page_size);
        if total_pages == 0 || want == 0 {
            return Self {
                pages: Vec::new(),
                next_cursor: 1,
            };
        }
        let needed = want
            .saturating_mul(2)
            .div_ceil(page_size)
            .clamp(1, MAX_PAGES_PER_CYCLE);
        let count = needed.min(total_pages);
        match selection {
            Selection::Random => Self::random(total_pages, count, rng),
            Selection::Sequential => Self::sequential(total_pages, count, cursor),
        }
    }

    fn random<R: RngExt>(total_pages: u32, count: u32, rng: &mut R) -> Self {
        let mut pages = Vec::with_capacity(count as usize);
        // Bounded rejection sampling: at most 8 * count draws.
        let max_draws = count.saturating_mul(8).max(1);
        let mut draws = 0;
        while u32::try_from(pages.len()).unwrap_or(u32::MAX) < count && draws < max_draws {
            draws = draws.saturating_add(1);
            let p = rng.random_range(1..=total_pages);
            if !pages.contains(&p) {
                pages.push(p);
            }
        }
        Self {
            pages,
            next_cursor: 1,
        }
    }

    fn sequential(total_pages: u32, count: u32, cursor: u32) -> Self {
        let start = if cursor == 0 || cursor > total_pages {
            1
        } else {
            cursor
        };
        let pages: Vec<u32> = (0..count)
            .map(|i| wrap_page(start.saturating_sub(1).saturating_add(i), total_pages))
            .collect();
        let next = pages.last().map_or(1, |last| wrap_page(*last, total_pages));
        Self {
            pages,
            next_cursor: next,
        }
    }
}

/// Maps a 0-based offset onto 1-based pages, wrapping around `total_pages` (> 0).
fn wrap_page(offset: u32, total_pages: u32) -> u32 {
    offset
        .checked_rem(total_pages)
        .unwrap_or(0)
        .saturating_add(1)
}

/// One wanted item as the worker sees it, normalised across *arr kinds.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Candidate {
    /// Entity id in the *arr app (episode, movie, album, book...).
    pub id: i64,
    /// Parent id (series, artist, author) used for grouping; equals `id` for movies.
    pub group: i64,
    /// Season number for episode-like items.
    pub season: Option<i32>,
    /// Human title for history and the UI.
    pub title: String,
    /// Release/air time as Unix seconds, if known.
    pub release_unix: Option<i64>,
    /// Whether the item (and its parent) is monitored.
    pub monitored: bool,
}

/// Filter rules applied before selection.
#[derive(Debug, Clone, Copy)]
pub struct Filter {
    /// Drop unmonitored items.
    pub monitored_only: bool,
    /// Drop items whose release is in the future (unknown release dates are kept).
    pub skip_future: bool,
    /// Current time as Unix seconds.
    pub now_unix: i64,
}

/// Applies [`Filter`], preserving order.
#[must_use]
pub fn filter(candidates: &[Candidate], f: &Filter) -> Vec<Candidate> {
    candidates
        .iter()
        .filter(|c| !(f.monitored_only && !c.monitored))
        .filter(|c| !(f.skip_future && c.release_unix.is_some_and(|r| r > f.now_unix)))
        .cloned()
        .collect()
}

/// Picks up to `want` candidates. Random selection is a partial Fisher-Yates shuffle so the
/// result is uniform without shuffling the whole slice.
#[must_use]
pub fn select<R: RngExt>(
    candidates: &[Candidate],
    want: usize,
    selection: Selection,
    rng: &mut R,
) -> Vec<Candidate> {
    let want = want.min(candidates.len());
    match selection {
        Selection::Sequential => candidates.iter().take(want).cloned().collect(),
        Selection::Random => {
            let mut pool: Vec<Candidate> = candidates.to_vec();
            for i in 0..want {
                let j = rng.random_range(i..pool.len());
                pool.swap(i, j);
            }
            pool.truncate(want);
            pool
        }
    }
}

/// Sonarr (and Whisparr v2) search granularity.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum SonarrMode {
    /// One `EpisodeSearch` per series with the selected episode ids.
    Episodes,
    /// One `SeasonSearch` per distinct series+season.
    SeasonPacks,
    /// One `SeriesSearch` per distinct series.
    Shows,
}

/// Lidarr search granularity.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum LidarrMode {
    /// One `ArtistSearch` per distinct artist.
    Artist,
    /// One `AlbumSearch` with the selected album ids.
    Album,
}

/// A search command to dispatch. `items` records which candidate ids it covers, which is
/// what the API stores as processed and what the hourly cap counts.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum Target {
    /// Sonarr/Whisparr v2 `EpisodeSearch`.
    Episodes {
        /// Parent series id.
        series_id: i64,
        /// Episode ids.
        items: Vec<i64>,
    },
    /// Sonarr/Whisparr v2 `SeasonSearch`.
    Season {
        /// Parent series id.
        series_id: i64,
        /// Season number.
        season: i32,
        /// Episode ids covered.
        items: Vec<i64>,
    },
    /// Sonarr/Whisparr v2 `SeriesSearch`.
    Series {
        /// Series id.
        series_id: i64,
        /// Episode ids covered.
        items: Vec<i64>,
    },
    /// Radarr/Whisparr v3 `MoviesSearch`, Lidarr `AlbumSearch`, Readarr `BookSearch`.
    Items {
        /// Entity ids.
        items: Vec<i64>,
    },
    /// Lidarr `ArtistSearch`.
    Artist {
        /// Artist id.
        artist_id: i64,
        /// Album ids covered.
        items: Vec<i64>,
    },
    /// Readarr `AuthorSearch`.
    Author {
        /// Author id.
        author_id: i64,
        /// Book ids covered.
        items: Vec<i64>,
    },
}

impl Target {
    /// Candidate ids this target covers.
    #[must_use]
    pub fn items(&self) -> &[i64] {
        match self {
            Self::Episodes { items, .. }
            | Self::Season { items, .. }
            | Self::Series { items, .. }
            | Self::Items { items }
            | Self::Artist { items, .. }
            | Self::Author { items, .. } => items,
        }
    }

    /// Number of items the hourly cap charges for this target.
    #[must_use]
    pub fn item_count(&self) -> u32 {
        u32::try_from(self.items().len()).unwrap_or(u32::MAX)
    }
}

fn by_group(selected: &[Candidate]) -> BTreeMap<i64, Vec<i64>> {
    let mut groups: BTreeMap<i64, Vec<i64>> = BTreeMap::new();
    for c in selected {
        groups.entry(c.group).or_default().push(c.id);
    }
    groups
}

/// Groups episode-like candidates into Sonarr search targets.
#[must_use]
pub fn group_sonarr(selected: &[Candidate], mode: SonarrMode) -> Vec<Target> {
    match mode {
        SonarrMode::Episodes => by_group(selected)
            .into_iter()
            .map(|(series_id, items)| Target::Episodes { series_id, items })
            .collect(),
        SonarrMode::Shows => by_group(selected)
            .into_iter()
            .map(|(series_id, items)| Target::Series { series_id, items })
            .collect(),
        SonarrMode::SeasonPacks => {
            let mut seasons: BTreeMap<(i64, i32), Vec<i64>> = BTreeMap::new();
            for c in selected {
                seasons
                    .entry((c.group, c.season.unwrap_or(0)))
                    .or_default()
                    .push(c.id);
            }
            seasons
                .into_iter()
                .map(|((series_id, season), items)| Target::Season {
                    series_id,
                    season,
                    items,
                })
                .collect()
        }
    }
}

/// Groups album candidates into Lidarr search targets.
#[must_use]
pub fn group_lidarr(selected: &[Candidate], mode: LidarrMode) -> Vec<Target> {
    match mode {
        LidarrMode::Album => group_flat(selected),
        LidarrMode::Artist => by_group(selected)
            .into_iter()
            .map(|(artist_id, items)| Target::Artist { artist_id, items })
            .collect(),
    }
}

/// Groups book candidates into one Readarr `BookSearch` per author.
#[must_use]
pub fn group_readarr(selected: &[Candidate]) -> Vec<Target> {
    by_group(selected)
        .into_iter()
        .map(|(author_id, items)| Target::Author { author_id, items })
        .collect()
}

/// One flat target holding every selected id (movies, albums in album mode).
#[must_use]
pub fn group_flat(selected: &[Candidate]) -> Vec<Target> {
    if selected.is_empty() {
        return Vec::new();
    }
    vec![Target::Items {
        items: selected.iter().map(|c| c.id).collect(),
    }]
}

/// Keeps whole targets while their cumulative item count fits into `granted`; the rest is
/// returned as deferred so the caller can report what the cap withheld.
#[must_use]
pub fn cap(targets: Vec<Target>, granted: u32) -> (Vec<Target>, Vec<Target>) {
    let mut used: u32 = 0;
    let mut dispatch = Vec::new();
    let mut deferred = Vec::new();
    for t in targets {
        let n = t.item_count();
        if used.saturating_add(n) <= granted {
            used = used.saturating_add(n);
            dispatch.push(t);
        } else {
            deferred.push(t);
        }
    }
    (dispatch, deferred)
}

#[cfg(test)]
mod tests {
    use std::collections::BTreeSet;

    use proptest::prelude::*;
    use rand::SeedableRng;
    use rand::rngs::SmallRng;

    use super::*;

    fn cand(id: i64, group: i64, season: i32, release: Option<i64>, monitored: bool) -> Candidate {
        Candidate {
            id,
            group,
            season: Some(season),
            title: format!("t{id}"),
            release_unix: release,
            monitored,
        }
    }

    fn arb_candidates() -> impl Strategy<Value = Vec<Candidate>> {
        prop::collection::vec(
            (
                1i64..10_000,
                1i64..50,
                1i32..20,
                prop::option::of(-1000i64..1000),
                any::<bool>(),
            )
                .prop_map(|(id, g, s, r, m)| cand(id, g, s, r, m)),
            0..64,
        )
        .prop_map(|mut v| {
            v.sort_by_key(|c| c.id);
            v.dedup_by_key(|c| c.id);
            v
        })
    }

    proptest! {
        #[test]
        fn plan_pages_are_distinct_and_in_range(total in 0u32..5000, size in 1u32..250, want in 0u32..100, cursor in 0u32..60, seed in any::<u64>()) {
            let mut rng = SmallRng::seed_from_u64(seed);
            for sel in [Selection::Random, Selection::Sequential] {
                let plan = PagePlan::plan(total, size, want, sel, cursor, &mut rng);
                let total_pages = total.div_ceil(size);
                let set: BTreeSet<u32> = plan.pages.iter().copied().collect();
                prop_assert_eq!(set.len(), plan.pages.len(), "pages must be distinct");
                prop_assert!(plan.pages.len() as u32 <= MAX_PAGES_PER_CYCLE.min(total_pages));
                prop_assert!(plan.pages.iter().all(|p| (1..=total_pages).contains(p)));
                if want > 0 && total_pages > 0 { prop_assert!(!plan.pages.is_empty()); }
                prop_assert!(plan.next_cursor >= 1);
            }
        }

        #[test]
        fn filter_never_keeps_future_or_unmonitored(cands in arb_candidates(), now in -1000i64..1000) {
            let f = Filter { monitored_only: true, skip_future: true, now_unix: now };
            let kept = filter(&cands, &f);
            prop_assert!(kept.iter().all(|c| c.monitored));
            prop_assert!(kept.iter().all(|c| c.release_unix.is_none_or(|r| r <= now)));
            prop_assert!(kept.len() <= cands.len());
        }

        #[test]
        fn select_is_a_subset_without_duplicates(cands in arb_candidates(), want in 0usize..80, seed in any::<u64>()) {
            let mut rng = SmallRng::seed_from_u64(seed);
            for sel in [Selection::Random, Selection::Sequential] {
                let picked = select(&cands, want, sel, &mut rng);
                prop_assert!(picked.len() <= want.min(cands.len()));
                let ids: BTreeSet<i64> = picked.iter().map(|c| c.id).collect();
                prop_assert_eq!(ids.len(), picked.len());
                prop_assert!(picked.iter().all(|p| cands.contains(p)));
            }
        }

        #[test]
        fn groupings_partition_the_selection(cands in arb_candidates()) {
            let all: BTreeSet<i64> = cands.iter().map(|c| c.id).collect();
            let plans = [
                group_sonarr(&cands, SonarrMode::Episodes),
                group_sonarr(&cands, SonarrMode::SeasonPacks),
                group_sonarr(&cands, SonarrMode::Shows),
                group_lidarr(&cands, LidarrMode::Artist),
                group_lidarr(&cands, LidarrMode::Album),
                group_readarr(&cands),
                group_flat(&cands),
            ];
            for targets in plans {
                let mut seen = Vec::new();
                for t in &targets { seen.extend_from_slice(t.items()); }
                let set: BTreeSet<i64> = seen.iter().copied().collect();
                prop_assert_eq!(set.len(), seen.len(), "no id appears in two targets");
                prop_assert_eq!(set, all.clone(), "every selected id is covered");
            }
        }

        #[test]
        fn cap_never_exceeds_grant(cands in arb_candidates(), granted in 0u32..40) {
            let targets = group_sonarr(&cands, SonarrMode::Episodes);
            let total: u32 = targets.iter().map(Target::item_count).sum();
            let (dispatch, deferred) = cap(targets, granted);
            let used: u32 = dispatch.iter().map(Target::item_count).sum();
            let left: u32 = deferred.iter().map(Target::item_count).sum();
            prop_assert!(used <= granted);
            prop_assert_eq!(used + left, total);
        }
    }

    #[test]
    fn sequential_plan_wraps_and_advances_cursor() {
        let mut rng = SmallRng::seed_from_u64(1);
        let plan = PagePlan::plan(1000, 100, 150, Selection::Sequential, 9, &mut rng);
        assert_eq!(plan.pages, vec![9, 10, 1]);
        assert_eq!(plan.next_cursor, 2);
    }

    #[test]
    fn season_packs_group_by_series_and_season() {
        let cands = vec![
            cand(1, 7, 1, None, true),
            cand(2, 7, 1, None, true),
            cand(3, 7, 2, None, true),
        ];
        let targets = group_sonarr(&cands, SonarrMode::SeasonPacks);
        assert_eq!(targets.len(), 2);
        assert!(
            matches!(&targets[0], Target::Season { series_id: 7, season: 1, items } if items == &vec![1, 2])
        );
    }

    #[test]
    fn boundary_empty_inputs() {
        let mut rng = SmallRng::seed_from_u64(0);
        assert!(
            PagePlan::plan(0, 100, 5, Selection::Random, 0, &mut rng)
                .pages
                .is_empty()
        );
        assert!(select(&[], 5, Selection::Random, &mut rng).is_empty());
        assert!(group_flat(&[]).is_empty());
        let (d, f) = cap(Vec::new(), 0);
        assert!(d.is_empty() && f.is_empty());
    }
}
