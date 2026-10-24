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
    resume_position, last_played_at, synced_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13
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
    synced_at = EXCLUDED.synced_at;

-- name: CountWatchAwareItemWatches :one
SELECT COUNT(*) FROM watchaware_item_watches;

-- name: CountWatchAwareItemWatchesForSource :one
SELECT COUNT(*) FROM watchaware_item_watches WHERE source_id = $1;

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
