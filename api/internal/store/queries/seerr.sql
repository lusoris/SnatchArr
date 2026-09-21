-- SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
-- SPDX-License-Identifier: EUPL-1.2

-- name: CreateSeerrLink :one
INSERT INTO seerr_links (id, name, base_url, api_key_enc, enabled, sonarr_instance_id, radarr_instance_id, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $8)
RETURNING *;

-- name: GetSeerrLink :one
SELECT * FROM seerr_links WHERE id = $1;

-- name: ListSeerrLinks :many
SELECT * FROM seerr_links ORDER BY name;

-- name: ListEnabledSeerrLinks :many
SELECT * FROM seerr_links WHERE enabled ORDER BY name;

-- name: UpdateSeerrLink :one
UPDATE seerr_links
SET name = $2, base_url = $3, api_key_enc = $4, enabled = $5, sonarr_instance_id = $6, radarr_instance_id = $7, updated_at = $8
WHERE id = $1
RETURNING *;

-- name: RecordSeerrCheck :exec
UPDATE seerr_links
SET last_seen_version = $2, last_check_at = $3, last_error = $4, updated_at = $3
WHERE id = $1;

-- name: RecordSeerrSync :exec
UPDATE seerr_links SET last_sync_at = $2, last_error = $3, updated_at = $2 WHERE id = $1;

-- name: DeleteSeerrLink :execrows
DELETE FROM seerr_links WHERE id = $1;

-- name: UpsertSeerrRequest :exec
INSERT INTO seerr_requests (
    link_id, request_id, media_type, tmdb_id, tvdb_id, title, request_status, media_status, is_4k,
    requested_by, seasons, seerr_server_id, instance_id, entity_id, requested_at, last_seen_at, updated_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9,
    $10, $11, $12, $13, $14, $15, $16, $16
)
ON CONFLICT (link_id, request_id) DO UPDATE SET
    media_type = EXCLUDED.media_type, tmdb_id = EXCLUDED.tmdb_id, tvdb_id = EXCLUDED.tvdb_id,
    title = CASE WHEN EXCLUDED.title = '' THEN seerr_requests.title ELSE EXCLUDED.title END,
    request_status = EXCLUDED.request_status, media_status = EXCLUDED.media_status, is_4k = EXCLUDED.is_4k,
    requested_by = EXCLUDED.requested_by, seasons = EXCLUDED.seasons, seerr_server_id = EXCLUDED.seerr_server_id,
    instance_id = EXCLUDED.instance_id, entity_id = EXCLUDED.entity_id, requested_at = EXCLUDED.requested_at,
    last_seen_at = EXCLUDED.last_seen_at, updated_at = EXCLUDED.updated_at;

-- name: GetSeerrRequestTitle :one
SELECT title FROM seerr_requests WHERE link_id = $1 AND request_id = $2;

-- name: DeleteUnseenSeerrRequests :execrows
DELETE FROM seerr_requests WHERE link_id = $1 AND last_seen_at < $2;

-- name: ListSeerrRequests :many
SELECT r.*, l.name AS link_name, i.name AS instance_name
FROM seerr_requests r
JOIN seerr_links l ON l.id = r.link_id
LEFT JOIN instances i ON i.id = r.instance_id
ORDER BY r.requested_at DESC NULLS LAST, r.request_id DESC
LIMIT $1;

-- name: GetSeerrRequest :one
SELECT * FROM seerr_requests WHERE link_id = $1 AND request_id = $2;

-- name: PriorityEntityIDs :many
SELECT DISTINCT entity_id::bigint AS entity_id
FROM seerr_requests
WHERE instance_id = $1 AND entity_id IS NOT NULL;

-- name: MarkSeerrRequestsSnatched :execrows
UPDATE seerr_requests
SET last_snatched_at = $2, updated_at = $2
WHERE instance_id = $1 AND entity_id = ANY(@entity_ids::bigint[]);

-- name: CountSeerrRequests :one
SELECT count(*) FROM seerr_requests WHERE link_id = $1;
