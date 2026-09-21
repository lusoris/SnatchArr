-- SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
-- SPDX-License-Identifier: EUPL-1.2

-- name: CreateInstance :one
INSERT INTO instances (id, kind, name, base_url, api_key_enc, enabled, source, configarr_key, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $9)
RETURNING *;

-- name: GetInstance :one
SELECT * FROM instances WHERE id = $1;

-- name: GetInstanceByConfigarrKey :one
SELECT * FROM instances WHERE configarr_key = $1;

-- name: ListInstances :many
SELECT * FROM instances ORDER BY kind, name;

-- name: ListEnabledInstances :many
SELECT * FROM instances WHERE enabled ORDER BY kind, name;

-- name: UpdateInstance :one
UPDATE instances
SET kind = $2, name = $3, base_url = $4, api_key_enc = $5, enabled = $6, updated_at = $7
WHERE id = $1
RETURNING *;

-- name: UpsertConfigarrInstance :one
INSERT INTO instances (id, kind, name, base_url, api_key_enc, enabled, source, configarr_key, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, 'configarr', $7, $8, $8)
ON CONFLICT (configarr_key) DO UPDATE
SET kind = EXCLUDED.kind, name = EXCLUDED.name, base_url = EXCLUDED.base_url,
    api_key_enc = EXCLUDED.api_key_enc, enabled = EXCLUDED.enabled, updated_at = EXCLUDED.updated_at
RETURNING *, (xmax = 0) AS inserted;

-- name: DisableConfigarrInstancesNotIn :execrows
UPDATE instances
SET enabled = FALSE, last_error = 'removed from the Configarr configuration', updated_at = sqlc.arg(updated_at)
WHERE source = 'configarr' AND enabled AND NOT (configarr_key = ANY(sqlc.arg(keep_keys)::text[]));

-- name: CountInstancesByBaseURLAndNotSource :one
SELECT count(*) FROM instances WHERE lower(base_url) = lower($1) AND source <> $2;

-- name: RecordInstanceCheck :exec
UPDATE instances
SET last_seen_version = $2, last_check_at = $3, last_error = $4, updated_at = $3
WHERE id = $1;

-- name: DeleteInstance :execrows
DELETE FROM instances WHERE id = $1;

-- name: CountInstancesByBaseURL :one
SELECT count(*) FROM instances WHERE lower(base_url) = lower($1) AND id <> $2;
