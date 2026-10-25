-- SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
-- SPDX-License-Identifier: EUPL-1.2

-- name: CreateWatchAwareSource :one
INSERT INTO watchaware_sources (
    id, kind, name, url, api_key_enc, enabled, created_at, updated_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8
)
RETURNING *;

-- name: GetWatchAwareSource :one
SELECT * FROM watchaware_sources WHERE id = $1;

-- name: ListWatchAwareSources :many
SELECT * FROM watchaware_sources ORDER BY name ASC;

-- name: ListEnabledWatchAwareSources :many
SELECT * FROM watchaware_sources WHERE enabled = TRUE ORDER BY name ASC;

-- name: UpdateWatchAwareSource :one
UPDATE watchaware_sources
SET name = $2, url = $3, api_key_enc = $4, enabled = $5, updated_at = $6
WHERE id = $1
RETURNING *;

-- name: RecordWatchAwareSourceSyncSuccess :exec
UPDATE watchaware_sources
SET last_sync_at = $2, last_error = NULL, updated_at = $2
WHERE id = $1;

-- name: RecordWatchAwareSourceSyncError :exec
UPDATE watchaware_sources
SET last_error = $2, updated_at = $3
WHERE id = $1;

-- name: DeleteWatchAwareSource :execrows
DELETE FROM watchaware_sources WHERE id = $1;

-- name: UpsertWatchAwareItemWatch :exec
INSERT INTO watchaware_item_watches (
    source_id, user_id, item_id, user_name, item_type, series_id,
    season_number, episode_number, played, played_percentage,
    resume_position, last_played_at, synced_at, provider_tvdb_id
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14
)
ON CONFLICT (source_id, user_id, item_id) DO UPDATE SET
    user_name = EXCLUDED.user_name,
    item_type = EXCLUDED.item_type,
    series_id = EXCLUDED.series_id,
    season_number = EXCLUDED.season_number,
    episode_number = EXCLUDED.episode_number,
    played = EXCLUDED.played,
    played_percentage = EXCLUDED.played_percentage,
    resume_position = EXCLUDED.resume_position,
    last_played_at = EXCLUDED.last_played_at,
    synced_at = EXCLUDED.synced_at,
    provider_tvdb_id = EXCLUDED.provider_tvdb_id;

-- name: CountWatchAwareItemWatches :one
SELECT COUNT(*) FROM watchaware_item_watches WHERE item_type != 'Series';

-- name: CountWatchAwareItemWatchesForSource :one
SELECT COUNT(*) FROM watchaware_item_watches WHERE source_id = $1 AND item_type != 'Series';

-- name: ListItemWatchesForSource :many
SELECT * FROM watchaware_item_watches WHERE source_id = $1 ORDER BY item_id ASC;

-- name: GetWatchAwareSyncSummary :one
SELECT
    COUNT(*)::bigint AS total_sources,
    COALESCE(COUNT(*) FILTER (WHERE enabled), 0)::bigint AS enabled_sources,
    COALESCE(MAX(last_sync_at), '0001-01-01 00:00:00Z'::timestamptz)::timestamptz AS last_sync_at
FROM watchaware_sources;

-- name: GetLatestWatchAwareSourceError :one
SELECT last_error
FROM watchaware_sources
WHERE last_error IS NOT NULL AND last_error != ''
ORDER BY updated_at DESC
LIMIT 1;

-- name: GetGateBaseline :one
SELECT * FROM watchaware_gate_baselines WHERE instance_id = $1;

-- name: InsertGateBaseline :exec
INSERT INTO watchaware_gate_baselines (
    instance_id, baselined_at, series_count
) VALUES (
    $1, $2, $3
)
ON CONFLICT (instance_id) DO NOTHING;

-- name: UpsertGateSeries :exec
INSERT INTO watchaware_gate_series (
    instance_id, series_id, tvdb_id, title, gate_season, state,
    added_before_gate, held_seasons, first_seen_at, updated_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10
)
ON CONFLICT (instance_id, series_id) DO UPDATE SET
    tvdb_id = EXCLUDED.tvdb_id,
    title = EXCLUDED.title,
    gate_season = EXCLUDED.gate_season,
    state = EXCLUDED.state,
    added_before_gate = EXCLUDED.added_before_gate,
    held_seasons = EXCLUDED.held_seasons,
    updated_at = EXCLUDED.updated_at;

-- name: UpdateGateSeriesTitle :exec
UPDATE watchaware_gate_series
SET title = $3, updated_at = $4
WHERE instance_id = $1 AND series_id = $2;

-- name: ListGateSeriesIDsForInstance :many
SELECT series_id FROM watchaware_gate_series WHERE instance_id = $1;

-- name: ListGateSeries :many
SELECT * FROM watchaware_gate_series
WHERE instance_id = $1
ORDER BY title ASC, series_id ASC
LIMIT $2;

-- name: GetGateSeries :one
SELECT * FROM watchaware_gate_series
WHERE instance_id = $1 AND series_id = $2;

-- name: UpsertGateDecision :exec
INSERT INTO watchaware_gate_decisions (
    id, instance_id, series_id, season_number, action, would_act, reason, decided_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8
)
ON CONFLICT (instance_id, series_id, season_number, action) DO UPDATE SET
    would_act = EXCLUDED.would_act,
    reason = EXCLUDED.reason,
    decided_at = EXCLUDED.decided_at,
    applied_at = CASE
        WHEN watchaware_gate_decisions.would_act = EXCLUDED.would_act THEN watchaware_gate_decisions.applied_at
        ELSE NULL
    END;

-- name: ListGateDecisions :many
SELECT * FROM watchaware_gate_decisions
WHERE (sqlc.narg(instance_id)::uuid IS NULL OR instance_id = sqlc.narg(instance_id)::uuid)
ORDER BY decided_at DESC
LIMIT $1;

-- name: PruneGateDecisions :execrows
DELETE FROM watchaware_gate_decisions
WHERE id IN (
    SELECT id FROM watchaware_gate_decisions
    WHERE decided_at < sqlc.arg(before)::timestamptz
    LIMIT sqlc.arg(max_rows)::int
);

-- name: ListGatedSeriesWithHeldSeasons :many
SELECT * FROM watchaware_gate_series
WHERE instance_id = $1
  AND state = 'gated'
  AND cardinality(held_seasons) > 0
ORDER BY series_id ASC
LIMIT $2;

-- name: ListEpisodeWatchesByTvdbIDs :many
SELECT
    s.provider_tvdb_id,
    e.source_id,
    e.user_id,
    e.season_number,
    e.episode_number,
    e.played,
    e.played_percentage
FROM watchaware_item_watches e
JOIN watchaware_item_watches s
    ON e.source_id = s.source_id
   AND e.user_id = s.user_id
   AND e.series_id = s.item_id
   AND s.item_type = 'Series'
WHERE e.item_type = 'Episode'
  AND s.provider_tvdb_id = ANY(@tvdb_ids::text[])
ORDER BY s.provider_tvdb_id, e.source_id, e.user_id, e.season_number, e.episode_number
LIMIT @max_rows;

-- name: UpdateGateSeriesHeldSeasons :exec
UPDATE watchaware_gate_series
SET held_seasons = $3, updated_at = $4
WHERE instance_id = $1 AND series_id = $2;

-- name: ListPendingGateDecisions :many
SELECT
    d.id,
    d.instance_id,
    d.series_id,
    d.season_number,
    d.action,
    d.would_act,
    d.reason,
    d.decided_at,
    d.applied_at
FROM watchaware_gate_decisions d
JOIN watchaware_gate_series s
    ON d.instance_id = s.instance_id
   AND d.series_id = s.series_id
WHERE d.instance_id = $1
  AND d.would_act = TRUE
  AND d.applied_at IS NULL
  AND NOT (s.state = 'released' AND d.action = 'unmonitor')
ORDER BY d.series_id ASC, d.season_number ASC
LIMIT @max_rows;

-- name: MarkGateDecisionsApplied :exec
UPDATE watchaware_gate_decisions
SET applied_at = sqlc.arg(applied_at)::timestamptz
WHERE id = ANY(sqlc.arg(ids)::uuid[]);

-- name: ReleaseGateSeries :one
UPDATE watchaware_gate_series
SET state = 'released',
    held_seasons = '{}',
    released_at = $3,
    updated_at = $3
WHERE instance_id = $1 AND series_id = $2
RETURNING *;
