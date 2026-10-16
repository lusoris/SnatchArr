// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

//! Pure snatch policy. No I/O, no clocks, no randomness of its own: callers pass `now` and an
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

use std::collections::{BTreeMap, BTreeSet};

use rand::RngExt;

/// How candidates are picked from a wanted list.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Selection {
    /// Random pages and random items: spreads searches across the library.
    Random,
    /// Walk the list in order from a cursor.
    Sequential,
    /// Newest releases first, with a slice of every pick reserved for the backlog.
    Recent,
}

/// Share of a recency selection that is drawn at random from the older backlog.
pub const BACKLOG_SHARE: usize = 5; // one in five

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
            // Recency needs a wide pool to sort, so it always reads from the front with the
            // full page allowance.
            Selection::Recent => {
                Self::sequential(total_pages, MAX_PAGES_PER_CYCLE.min(total_pages), 1)
            }
        }
    }

    /// Reads the list from the front with the full page allowance: for focused runs that
    /// must find one specific entity.
    #[must_use]
    pub fn sweep(total_records: u32, page_size: u32) -> Self {
        let total_pages = total_records.div_ceil(page_size.max(1));
        if total_pages == 0 {
            return Self {
                pages: Vec::new(),
                next_cursor: 1,
            };
        }
        Self::sequential(total_pages, MAX_PAGES_PER_CYCLE.min(total_pages), 1)
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
    /// *arr tag ids of the item, or of its parent where the tags live there.
    pub tags: Vec<i32>,
    /// Quality profile ids that apply to the item; more than one where an app keeps one per
    /// format (Chaptarr's audiobook and ebook profiles).
    pub profiles: Vec<i32>,
}

/// An include and an exclude list of *arr ids (tags or quality profiles).
#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct IdRule {
    /// When not empty, an item must carry at least one of these ids.
    pub include: Vec<i32>,
    /// An item that carries any of these ids is dropped; exclude wins over include.
    pub exclude: Vec<i32>,
}

impl IdRule {
    /// Whether an item carrying `ids` passes the rule.
    #[must_use]
    pub fn admits(&self, ids: &[i32]) -> bool {
        let hit = |list: &[i32]| ids.iter().any(|i| list.contains(i));
        (self.include.is_empty() || hit(&self.include)) && !hit(&self.exclude)
    }
}

/// Filter rules applied before selection.
#[derive(Debug, Clone, Default)]
pub struct Filter {
    /// Drop unmonitored items.
    pub monitored_only: bool,
    /// Drop items whose release is in the future (unknown release dates are kept).
    pub skip_future: bool,
    /// Drop items released less than this many seconds ago; 0 turns the delay off. Future
    /// releases fall under the delay too; unknown release dates are kept.
    pub release_delay_secs: i64,
    /// Current time as Unix seconds.
    pub now_unix: i64,
    /// Tag ids an item must or must not carry.
    pub tags: IdRule,
    /// Quality profile ids an item must or must not use.
    pub profiles: IdRule,
}

impl Filter {
    /// Whether one candidate passes every rule.
    #[must_use]
    pub fn admits(&self, c: &Candidate) -> bool {
        let released_by = |at: i64| c.release_unix.is_none_or(|r| r <= at);
        (!self.monitored_only || c.monitored)
            && (!self.skip_future || released_by(self.now_unix))
            && (self.release_delay_secs <= 0
                || released_by(self.now_unix.saturating_sub(self.release_delay_secs)))
            && self.tags.admits(&c.tags)
            && self.profiles.admits(&c.profiles)
    }
}

/// Applies [`Filter`], preserving order.
#[must_use]
pub fn filter(candidates: &[Candidate], f: &Filter) -> Vec<Candidate> {
    candidates.iter().filter(|c| f.admits(c)).cloned().collect()
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
            partial_shuffle(&mut pool, want, rng);
            pool.truncate(want);
            pool
        }
        Selection::Recent => select_recent(candidates, want, rng),
    }
}

/// Like [`select`], but candidates whose id is in `priority_ids` or whose group is in
/// `priority_groups` (open Seerr requests) are picked before anything else. Within each
/// half the normal selection applies.
#[must_use]
pub fn select_prioritised<R: RngExt>(
    candidates: &[Candidate],
    want: usize,
    selection: Selection,
    priority_ids: &[i64],
    priority_groups: &[i64],
    rng: &mut R,
) -> Vec<Candidate> {
    if priority_ids.is_empty() && priority_groups.is_empty() {
        return select(candidates, want, selection, rng);
    }
    let (first, rest): (Vec<Candidate>, Vec<Candidate>) = candidates
        .iter()
        .cloned()
        .partition(|c| priority_ids.contains(&c.id) || priority_groups.contains(&c.group));
    let mut picked = select(&first, want, selection, rng);
    let remaining = want.saturating_sub(picked.len());
    picked.extend(select(&rest, remaining, selection, rng));
    picked
}

/// Moves `want` uniformly chosen elements to the front (partial Fisher-Yates).
fn partial_shuffle<R: RngExt>(pool: &mut [Candidate], want: usize, rng: &mut R) {
    for i in 0..want.min(pool.len()) {
        let j = rng.random_range(i..pool.len());
        pool.swap(i, j);
    }
}

/// Newest releases first (unknown dates last); one pick in [`BACKLOG_SHARE`] is drawn at
/// random from whatever is left so an old backlog still gets attention.
fn select_recent<R: RngExt>(candidates: &[Candidate], want: usize, rng: &mut R) -> Vec<Candidate> {
    let mut pool: Vec<Candidate> = candidates.to_vec();
    // `None < Some(_)`, so a descending sort puts the newest first and undated last.
    pool.sort_by_key(|c| std::cmp::Reverse(c.release_unix));
    let backlog = want.checked_div(BACKLOG_SHARE).unwrap_or(0);
    let newest = want.saturating_sub(backlog).min(pool.len());
    let mut picked: Vec<Candidate> = pool.drain(..newest).collect();
    let extra = backlog.min(pool.len());
    partial_shuffle(&mut pool, extra, rng);
    picked.extend(pool.into_iter().take(extra));
    picked
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
        /// Distinct seasons among the covered episodes (one indexer query each).
        seasons: u32,
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

    /// Number of candidate items the target covers (what afterglow remembers).
    #[must_use]
    pub fn item_count(&self) -> u32 {
        u32::try_from(self.items().len()).unwrap_or(u32::MAX)
    }

    /// Whether the target is one command for a whole parent (`Season`, `Series`, `Artist`,
    /// `Author`). It cannot be split, so stamina grants it whole or not at all (#142).
    #[must_use]
    pub fn atomic(&self) -> bool {
        !matches!(self, Self::Items { .. } | Self::Episodes { .. })
    }

    /// Searches the *arr sends to each indexer for the target, which is what stamina charges
    /// (#180): one per episode in an `EpisodeSearch`, one per season pack, one per season of
    /// a `SeriesSearch`, one per movie, album or book, and one for a whole `ArtistSearch` or
    /// `AuthorSearch`, which Lidarr and the Readarr forks send as a single search over the
    /// parent's monitored albums or books.
    #[must_use]
    pub fn query_cost(&self) -> u32 {
        match self {
            Self::Season { .. } | Self::Artist { .. } | Self::Author { .. } => 1,
            Self::Series { seasons, .. } => (*seasons).max(1),
            Self::Episodes { .. } | Self::Items { .. } => self.item_count().max(1),
        }
    }

    /// Splits a target whose items are searched one by one into its first `keep` items and
    /// the rest. Returns the target unchanged as `Err` when it cannot be split: it is one
    /// command for a whole parent, or `keep` is 0 or covers every item.
    ///
    /// # Errors
    ///
    /// The unsplit target, so the caller keeps ownership.
    pub fn split(self, keep: u32) -> Result<(Self, Self), Self> {
        if keep == 0 || keep >= self.item_count() {
            return Err(self);
        }
        let keep = usize::try_from(keep).unwrap_or(usize::MAX);
        match self {
            Self::Items { mut items } => {
                let rest = items.split_off(keep);
                Ok((Self::Items { items }, Self::Items { items: rest }))
            }
            Self::Episodes {
                series_id,
                mut items,
            } => {
                let rest = items.split_off(keep);
                Ok((
                    Self::Episodes { series_id, items },
                    Self::Episodes {
                        series_id,
                        items: rest,
                    },
                ))
            }
            whole => Err(whole),
        }
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
        SonarrMode::Shows => {
            let mut seasons: BTreeMap<i64, BTreeSet<i32>> = BTreeMap::new();
            for c in selected {
                seasons
                    .entry(c.group)
                    .or_default()
                    .insert(c.season.unwrap_or(0));
            }
            by_group(selected)
                .into_iter()
                .map(|(series_id, items)| Target::Series {
                    series_id,
                    seasons: seasons
                        .get(&series_id)
                        .map_or(1, |s| u32::try_from(s.len()).unwrap_or(u32::MAX)),
                    items,
                })
                .collect()
        }
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

/// Groups book candidates into one Readarr `AuthorSearch` per author. A Readarr run never
/// sends a `BookSearch`.
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

/// Applies the per-target grants of one `AcquireBudget` answer, in target order (#142). A
/// full grant dispatches the target. A partial grant, which the API only gives to a target
/// searched item by item, dispatches that many items and defers the rest. No grant defers
/// the target. A partial grant for a whole-parent target dispatches nothing and a missing
/// grant counts as zero, so the dispatched cost never exceeds what was granted.
#[must_use]
pub fn apply_grants(targets: Vec<Target>, grants: &[u32]) -> (Vec<Target>, Vec<Target>) {
    let mut dispatch = Vec::new();
    let mut deferred = Vec::new();
    let grants = grants.iter().copied().chain(std::iter::repeat(0));
    for (target, grant) in targets.into_iter().zip(grants) {
        if grant >= target.query_cost() {
            dispatch.push(target);
            continue;
        }
        match target.split(grant) {
            Ok((head, tail)) => {
                dispatch.push(head);
                deferred.push(tail);
            }
            Err(whole) => deferred.push(whole),
        }
    }
    (dispatch, deferred)
}

/// Keeps targets while their cumulative query cost fits into `granted` and returns the rest
/// as deferred so the caller can report what stamina withheld. A target whose items are
/// searched one by one (`Items`, `Episodes`) is split at the grant: the API has already
/// debited what it granted, so leaving it unused would lose stamina (#136). A target that is
/// one command for a whole parent stays whole. This is the path for an API that answers
/// without per-target grants.
#[must_use]
pub fn cap(targets: Vec<Target>, granted: u32) -> (Vec<Target>, Vec<Target>) {
    let mut used: u32 = 0;
    let mut dispatch = Vec::new();
    let mut deferred = Vec::new();
    for t in targets {
        let n = t.query_cost();
        let left = granted.saturating_sub(used);
        if n <= left {
            used = used.saturating_add(n);
            dispatch.push(t);
            continue;
        }
        match t.split(left) {
            Ok((head, tail)) => {
                used = used.saturating_add(head.query_cost());
                dispatch.push(head);
                deferred.push(tail);
            }
            Err(whole) => deferred.push(whole),
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
            tags: Vec::new(),
            profiles: Vec::new(),
        }
    }

    // Small id ranges so include and exclude lists hit the candidates' ids often.
    fn arb_ids() -> impl Strategy<Value = Vec<i32>> {
        prop::collection::vec(1i32..6, 0..3)
    }

    fn arb_rule() -> impl Strategy<Value = IdRule> {
        (arb_ids(), arb_ids()).prop_map(|(include, exclude)| IdRule { include, exclude })
    }

    fn arb_tagged() -> impl Strategy<Value = Vec<Candidate>> {
        (
            arb_candidates(),
            prop::collection::vec((arb_ids(), arb_ids()), 64),
        )
            .prop_map(|(mut cands, ids)| {
                for (c, (tags, profiles)) in cands.iter_mut().zip(ids) {
                    c.tags = tags;
                    c.profiles = profiles;
                }
                cands
            })
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

    // Targets of every shape: flat items, episodes per series, season packs, whole series.
    fn arb_targets() -> impl Strategy<Value = Vec<Target>> {
        (arb_candidates(), 0u8..4).prop_map(|(cands, shape)| match shape {
            0 => group_flat(&cands),
            1 => group_sonarr(&cands, SonarrMode::Episodes),
            2 => group_sonarr(&cands, SonarrMode::SeasonPacks),
            _ => group_sonarr(&cands, SonarrMode::Shows),
        })
    }

    // The API's rule, restated for the tests: in order, a target that fits is granted its
    // cost, one searched item by item gets what is left, a whole parent that does not fit
    // gets nothing.
    fn greedy_grants(targets: &[Target], mut left: u32) -> Vec<u32> {
        targets
            .iter()
            .map(|t| {
                let cost = t.query_cost();
                let grant = if cost <= left {
                    cost
                } else if t.atomic() {
                    0
                } else {
                    left
                };
                left -= grant;
                grant
            })
            .collect()
    }

    proptest! {
        #[test]
        fn plan_pages_are_distinct_and_in_range(total in 0u32..5000, size in 1u32..250, want in 0u32..100, cursor in 0u32..60, seed in any::<u64>()) {
            let mut rng = SmallRng::seed_from_u64(seed);
            for sel in [Selection::Random, Selection::Sequential, Selection::Recent] {
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
            let f = Filter { monitored_only: true, skip_future: true, now_unix: now, ..Filter::default() };
            let kept = filter(&cands, &f);
            prop_assert!(kept.iter().all(|c| c.monitored));
            prop_assert!(kept.iter().all(|c| c.release_unix.is_none_or(|r| r <= now)));
            prop_assert!(kept.len() <= cands.len());
        }

        // Every kept item carries an included tag (when tags are included) and no excluded one.
        #[test]
        fn tag_filter_keeps_only_included_and_never_excluded(cands in arb_tagged(), tags in arb_rule()) {
            let f = Filter { tags: tags.clone(), ..Filter::default() };
            for c in filter(&cands, &f) {
                prop_assert!(tags.include.is_empty() || c.tags.iter().any(|t| tags.include.contains(t)));
                prop_assert!(!c.tags.iter().any(|t| tags.exclude.contains(t)));
            }
        }

        #[test]
        fn profile_filter_keeps_only_included_and_never_excluded(cands in arb_tagged(), profiles in arb_rule()) {
            let f = Filter { profiles: profiles.clone(), ..Filter::default() };
            for c in filter(&cands, &f) {
                prop_assert!(profiles.include.is_empty() || c.profiles.iter().any(|p| profiles.include.contains(p)));
                prop_assert!(!c.profiles.iter().any(|p| profiles.exclude.contains(p)));
            }
        }

        // Nothing is kept that was released less than the delay ago, future releases included;
        // an unknown release date is kept.
        #[test]
        fn release_delay_keeps_only_items_released_long_enough_ago(cands in arb_candidates(), now in -1000i64..1000, delay in 1i64..500) {
            let f = Filter { release_delay_secs: delay, now_unix: now, ..Filter::default() };
            let kept = filter(&cands, &f);
            prop_assert!(kept.iter().all(|c| c.release_unix.is_none_or(|r| r + delay <= now)));
            let unknown = cands.iter().filter(|c| c.release_unix.is_none()).count();
            prop_assert!(kept.iter().filter(|c| c.release_unix.is_none()).count() == unknown);
        }

        // The filter drops no more than its rules ask for: an item that passes every rule,
        // restated here independently, is kept, in its original order.
        #[test]
        fn filter_keeps_every_item_that_passes_all_rules(
            cands in arb_tagged(), now in -1000i64..1000, delay in 0i64..500,
            monitored_only in any::<bool>(), skip_future in any::<bool>(),
            tags in arb_rule(), profiles in arb_rule(),
        ) {
            let f = Filter { monitored_only, skip_future, release_delay_secs: delay, now_unix: now, tags: tags.clone(), profiles: profiles.clone() };
            let hit = |ids: &[i32], list: &[i32]| ids.iter().any(|i| list.contains(i));
            let expected: Vec<Candidate> = cands.iter().filter(|c| {
                (!monitored_only || c.monitored)
                    && !(skip_future && c.release_unix.is_some_and(|r| r > now))
                    && !(delay > 0 && c.release_unix.is_some_and(|r| r + delay > now))
                    && (tags.include.is_empty() || hit(&c.tags, &tags.include))
                    && !hit(&c.tags, &tags.exclude)
                    && (profiles.include.is_empty() || hit(&c.profiles, &profiles.include))
                    && !hit(&c.profiles, &profiles.exclude)
            }).cloned().collect();
            prop_assert_eq!(filter(&cands, &f), expected);
        }

        // Boundary: rules left empty keep every candidate.
        #[test]
        fn empty_rules_keep_everything(cands in arb_tagged(), now in -1000i64..1000) {
            let f = Filter { now_unix: now, ..Filter::default() };
            prop_assert_eq!(filter(&cands, &f), cands);
        }

        #[test]
        fn select_is_a_subset_without_duplicates(cands in arb_candidates(), want in 0usize..80, seed in any::<u64>()) {
            let mut rng = SmallRng::seed_from_u64(seed);
            for sel in [Selection::Random, Selection::Sequential, Selection::Recent] {
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

        // With grants as the API computes them, the worker searches exactly what was debited
        // and loses no item (#142).
        #[test]
        fn apply_grants_spends_exactly_what_the_api_granted(targets in arb_targets(), left in 0u32..40) {
            let grants = greedy_grants(&targets, left);
            let mut before: Vec<i64> = targets.iter().flat_map(|t| t.items().to_vec()).collect();
            let (dispatch, deferred) = apply_grants(targets, &grants);
            let used: u32 = dispatch.iter().map(Target::query_cost).sum();
            prop_assert_eq!(used, grants.iter().sum::<u32>());
            prop_assert!(used <= left);
            prop_assert!(dispatch.iter().chain(&deferred).all(|t| t.item_count() > 0), "no empty target");
            let mut after: Vec<i64> = dispatch.iter().chain(&deferred).flat_map(|t| t.items().to_vec()).collect();
            before.sort_unstable();
            after.sort_unstable();
            prop_assert_eq!(before, after);
        }

        // Whatever the answer holds (short, long, partial grants for whole parents), the
        // dispatched cost stays within the grants and no item is lost or duplicated.
        #[test]
        fn apply_grants_never_exceeds_any_grant(targets in arb_targets(), grants in prop::collection::vec(0u32..20, 0..80)) {
            let budget: u32 = grants.iter().take(targets.len()).sum();
            let mut before: Vec<i64> = targets.iter().flat_map(|t| t.items().to_vec()).collect();
            let (dispatch, deferred) = apply_grants(targets, &grants);
            let used: u32 = dispatch.iter().map(Target::query_cost).sum();
            prop_assert!(used <= budget, "dispatched {} of {} granted", used, budget);
            let mut after: Vec<i64> = dispatch.iter().chain(&deferred).flat_map(|t| t.items().to_vec()).collect();
            before.sort_unstable();
            after.sort_unstable();
            prop_assert_eq!(before, after);
        }

        #[test]
        fn cap_never_exceeds_grant(cands in arb_candidates(), granted in 0u32..40) {
            let targets = group_sonarr(&cands, SonarrMode::Episodes);
            let total: u32 = targets.iter().map(Target::query_cost).sum();
            let (dispatch, deferred) = cap(targets, granted);
            let used: u32 = dispatch.iter().map(Target::query_cost).sum();
            let left: u32 = deferred.iter().map(Target::query_cost).sum();
            prop_assert!(used <= granted);
            prop_assert_eq!(used + left, total);
        }

        // Targets searched item by item use the grant exactly and lose no item (#136).
        #[test]
        fn cap_spends_the_whole_grant_on_item_targets(cands in arb_candidates(), granted in 0u32..40, flat in any::<bool>()) {
            let targets = if flat { group_flat(&cands) } else { group_sonarr(&cands, SonarrMode::Episodes) };
            let total: u32 = targets.iter().map(Target::query_cost).sum();
            let mut before: Vec<i64> = targets.iter().flat_map(|t| t.items().to_vec()).collect();
            let (dispatch, deferred) = cap(targets, granted);
            let used: u32 = dispatch.iter().map(Target::query_cost).sum();
            prop_assert_eq!(used, granted.min(total));
            prop_assert!(dispatch.iter().chain(&deferred).all(|t| t.item_count() > 0), "no empty target");
            let mut after: Vec<i64> = dispatch.iter().chain(&deferred).flat_map(|t| t.items().to_vec()).collect();
            before.sort_unstable();
            after.sort_unstable();
            prop_assert_eq!(before, after);
        }
    }

    // A grant smaller than one multi-item target still searches as many items as granted:
    // the API has already debited the grant, so dispatching nothing would lose stamina (#136).
    #[test]
    fn cap_splits_an_item_target_to_the_grant() {
        let cands: Vec<_> = (1..=5).map(|i| cand(i, i, 1, None, true)).collect();
        let (dispatch, deferred) = cap(group_flat(&cands), 3);
        assert_eq!(dispatch.iter().map(Target::query_cost).sum::<u32>(), 3);
        assert_eq!(deferred.iter().map(Target::query_cost).sum::<u32>(), 2);
        let mut all: Vec<i64> = dispatch
            .iter()
            .chain(&deferred)
            .flat_map(|t| t.items().to_vec())
            .collect();
        all.sort_unstable();
        assert_eq!(
            all,
            vec![1, 2, 3, 4, 5],
            "every item is dispatched or deferred exactly once"
        );
    }

    // Negative: one command for a whole parent is never split, so it waits whole; a smaller
    // target after it still uses what is left.
    #[test]
    fn cap_keeps_whole_parent_targets_whole() {
        let series = Target::Series {
            series_id: 1,
            seasons: 2,
            items: vec![1, 2, 3],
        };
        let pack = Target::Season {
            series_id: 2,
            season: 1,
            items: vec![4, 5],
        };
        let (dispatch, deferred) = cap(vec![series.clone(), pack.clone()], 1);
        assert_eq!(dispatch, vec![pack]);
        assert_eq!(deferred, vec![series]);
    }

    // Boundary: no grant defers everything unsplit; a grant equal to the cost keeps it whole.
    #[test]
    fn cap_boundaries_do_not_split() {
        let cands: Vec<_> = (1..=3).map(|i| cand(i, i, 1, None, true)).collect();
        let (dispatch, deferred) = cap(group_flat(&cands), 0);
        assert!(dispatch.is_empty());
        assert_eq!(deferred, group_flat(&cands));
        let (dispatch, deferred) = cap(group_flat(&cands), 3);
        assert_eq!(dispatch, group_flat(&cands));
        assert!(deferred.is_empty());
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

    /// A wanted book is searched through its author: one `Target::Author` per author with the
    /// author's wanted books, in author order (docs/readarr.md).
    #[test]
    fn readarr_books_group_into_one_author_search_per_author() {
        let cands = vec![
            cand(1, 7, 0, None, true),
            cand(2, 9, 0, None, true),
            cand(3, 7, 0, None, true),
        ];
        let by_author = vec![
            Target::Author {
                author_id: 7,
                items: vec![1, 3],
            },
            Target::Author {
                author_id: 9,
                items: vec![2],
            },
        ];
        assert_eq!(group_readarr(&cands), by_author);
        // Boundary: one book is still an author search, and no book is no search.
        let single = vec![Target::Author {
            author_id: 7,
            items: vec![1],
        }];
        assert_eq!(group_readarr(&cands[..1]), single);
        assert!(group_readarr(&[]).is_empty());
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

    #[test]
    fn recent_prefers_newest_and_keeps_a_backlog_slice() {
        let mut rng = SmallRng::seed_from_u64(7);
        let cands: Vec<Candidate> = (1..=10)
            .map(|i| cand(i, 1, 1, Some(i * 100), true))
            .chain([cand(99, 1, 1, None, true)])
            .collect();
        let picked = select(&cands, 5, Selection::Recent, &mut rng);
        assert_eq!(picked.len(), 5);
        let newest: Vec<i64> = picked.iter().take(4).map(|c| c.id).collect();
        assert_eq!(
            newest,
            vec![10, 9, 8, 7],
            "four of five picks are the newest"
        );
        assert!(
            picked[4].id <= 6 || picked[4].id == 99,
            "the fifth comes from the backlog"
        );
        assert!(select(&cands, 1, Selection::Recent, &mut rng)[0].id == 10);
    }

    #[test]
    fn prioritised_selection_puts_requests_first() {
        let mut rng = SmallRng::seed_from_u64(3);
        let cands: Vec<Candidate> = (1..=20).map(|i| cand(i, i % 4, 1, Some(i), true)).collect();
        let picked = select_prioritised(&cands, 3, Selection::Random, &[7], &[2], &mut rng);
        assert_eq!(picked.len(), 3);
        assert!(
            picked.iter().all(|c| c.id == 7 || c.group == 2),
            "all picks are requested: {picked:?}"
        );
        let few = select_prioritised(&cands, 8, Selection::Sequential, &[7], &[], &mut rng);
        assert_eq!(few[0].id, 7, "the requested movie comes first");
        assert_eq!(few.len(), 8);
        assert_eq!(
            select_prioritised(&cands, 2, Selection::Sequential, &[], &[], &mut rng),
            select(&cands, 2, Selection::Sequential, &mut rng)
        );
    }

    #[test]
    fn sweep_reads_from_the_front() {
        let plan = PagePlan::sweep(2500, 500);
        assert_eq!(plan.pages, vec![1, 2, 3, 4, 5]);
        assert_eq!(PagePlan::sweep(0, 500).pages.len(), 0);
        assert_eq!(
            PagePlan::sweep(100_000, 100).pages.len() as u32,
            MAX_PAGES_PER_CYCLE
        );
    }

    #[test]
    fn query_cost_charges_indexer_queries() {
        let eps = vec![
            cand(1, 1, 1, None, true),
            cand(2, 1, 2, None, true),
            cand(3, 1, 2, None, true),
        ];
        let by_episode = group_sonarr(&eps, SonarrMode::Episodes);
        assert_eq!(by_episode.iter().map(Target::query_cost).sum::<u32>(), 3);
        let packs = group_sonarr(&eps, SonarrMode::SeasonPacks);
        assert_eq!(packs.len(), 2);
        assert_eq!(packs.iter().map(Target::query_cost).sum::<u32>(), 2);
        let shows = group_sonarr(&eps, SonarrMode::Shows);
        assert_eq!(shows.len(), 1);
        assert_eq!(
            shows[0].query_cost(),
            2,
            "a series search costs one query per season"
        );
        assert_eq!(shows[0].item_count(), 3);
    }
    /// An `ArtistSearch` or `AuthorSearch` is one search per indexer however many wanted
    /// albums or books it covers (Lidarr `ReleaseSearchService.ArtistSearch`, the Readarr
    /// forks' `AuthorSearch`: one dispatch each), so stamina charges it 1 (#180).
    #[test]
    fn query_cost_charges_parent_searches_once() {
        let cands = vec![
            cand(1, 7, 0, None, true),
            cand(2, 7, 0, None, true),
            cand(3, 7, 0, None, true),
            cand(4, 9, 0, None, true),
        ];
        let artists = group_lidarr(&cands, LidarrMode::Artist);
        assert_eq!(artists.len(), 2);
        assert_eq!(artists[0].item_count(), 3);
        assert_eq!(
            artists[0].query_cost(),
            1,
            "one artist search for three albums"
        );
        assert_eq!(artists.iter().map(Target::query_cost).sum::<u32>(), 2);
        let authors = group_readarr(&cands);
        assert_eq!(
            authors[0].query_cost(),
            1,
            "one author search for three books"
        );
        assert_eq!(authors.iter().map(Target::query_cost).sum::<u32>(), 2);
        // Album mode still searches each album on its own.
        let albums = group_lidarr(&cands, LidarrMode::Album);
        assert_eq!(albums.iter().map(Target::query_cost).sum::<u32>(), 4);
        // Boundary: an author with one book and one with none both cost one search.
        let one = group_readarr(&cands[3..]);
        assert_eq!(one.iter().map(Target::query_cost).sum::<u32>(), 1);
        let empty = Target::Author {
            author_id: 1,
            items: Vec::new(),
        };
        assert_eq!(empty.query_cost(), 1);
    }

    fn series(series_id: i64, seasons: u32, items: Vec<i64>) -> Target {
        Target::Series {
            series_id,
            seasons,
            items,
        }
    }

    // Positive: a full grant dispatches the target whole; a partial grant on an item target
    // dispatches exactly that many items.
    #[test]
    fn apply_grants_dispatches_full_and_partial_grants() {
        let whole = series(1, 2, vec![1, 2, 3]);
        let episodes = Target::Episodes {
            series_id: 2,
            items: vec![4, 5, 6],
        };
        let (dispatch, deferred) = apply_grants(vec![whole.clone(), episodes], &[2, 1]);
        let head = Target::Episodes {
            series_id: 2,
            items: vec![4],
        };
        let tail = Target::Episodes {
            series_id: 2,
            items: vec![5, 6],
        };
        assert_eq!(dispatch, vec![whole, head]);
        assert_eq!(deferred, vec![tail]);
    }

    // Negative: a whole-parent target is never searched on a partial or missing grant, and a
    // smaller target after it still uses its own grant.
    #[test]
    fn apply_grants_keeps_whole_parent_targets_whole() {
        let whole = series(1, 2, vec![1, 2, 3]);
        let pack = Target::Season {
            series_id: 2,
            season: 1,
            items: vec![4, 5],
        };
        let (dispatch, deferred) = apply_grants(vec![whole.clone(), pack.clone()], &[1, 1]);
        assert_eq!(dispatch, vec![pack.clone()]);
        assert_eq!(deferred, vec![whole.clone()]);

        let (dispatch, deferred) = apply_grants(vec![whole.clone(), pack.clone()], &[]);
        assert!(dispatch.is_empty(), "a missing grant is no grant");
        assert_eq!(deferred, vec![whole, pack]);
    }

    // Boundary: zero grants defer everything; a grant above the cost dispatches the target
    // once, never more than it holds.
    #[test]
    fn apply_grants_boundaries() {
        let items = Target::Items { items: vec![1, 2] };
        let (dispatch, deferred) = apply_grants(vec![items.clone()], &[0]);
        assert!(dispatch.is_empty());
        assert_eq!(deferred, vec![items.clone()]);

        let (dispatch, deferred) = apply_grants(vec![items.clone()], &[9]);
        assert_eq!(dispatch, vec![items]);
        assert!(deferred.is_empty());

        let (dispatch, deferred) = apply_grants(Vec::new(), &[3]);
        assert!(dispatch.is_empty() && deferred.is_empty());
    }

    #[test]
    fn atomic_marks_whole_parent_targets() {
        assert!(series(1, 1, vec![1]).atomic());
        assert!(
            Target::Season {
                series_id: 1,
                season: 1,
                items: vec![1]
            }
            .atomic()
        );
        assert!(!Target::Items { items: vec![1] }.atomic());
        assert!(
            !Target::Episodes {
                series_id: 1,
                items: vec![1]
            }
            .atomic()
        );
    }
}
