-- SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
-- SPDX-License-Identifier: EUPL-1.2

-- name: CreateProwlarrLink :one
INSERT INTO prowlarr_links (id, name, base_url, api_key_enc, enabled, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $6)
RETURNING *;

-- name: GetProwlarrLink :one
SELECT * FROM prowlarr_links WHERE id = $1;

-- name: ListProwlarrLinks :many
SELECT * FROM prowlarr_links ORDER BY name;

-- name: ListEnabledProwlarrLinks :many
SELECT * FROM prowlarr_links WHERE enabled ORDER BY name;

-- name: UpdateProwlarrLink :one
UPDATE prowlarr_links
SET name = $2, base_url = $3, api_key_enc = $4, enabled = $5, updated_at = $6
WHERE id = $1
RETURNING *;

-- name: RecordProwlarrCheck :exec
UPDATE prowlarr_links
SET last_seen_version = $2, last_check_at = $3, last_error = $4, updated_at = $3
WHERE id = $1;

-- name: DeleteProwlarrLink :execrows
DELETE FROM prowlarr_links WHERE id = $1;
