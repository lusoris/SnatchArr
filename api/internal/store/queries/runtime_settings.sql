-- SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
-- SPDX-License-Identifier: EUPL-1.2

-- name: ListRuntimeSettings :many
SELECT * FROM runtime_settings ORDER BY key;

-- name: GetRuntimeSetting :one
SELECT * FROM runtime_settings WHERE key = $1;

-- name: UpsertRuntimeSetting :one
INSERT INTO runtime_settings (key, value, updated_by, updated_at)
VALUES ($1, $2, $3, $4)
ON CONFLICT (key) DO UPDATE
SET value = EXCLUDED.value,
    updated_by = EXCLUDED.updated_by,
    updated_at = EXCLUDED.updated_at
RETURNING *;

-- name: DeleteRuntimeSetting :exec
DELETE FROM runtime_settings WHERE key = $1;
