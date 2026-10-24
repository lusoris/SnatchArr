-- SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
-- SPDX-License-Identifier: EUPL-1.2

-- name: GetModule :one
SELECT * FROM modules WHERE id = $1;

-- name: ListModules :many
SELECT * FROM modules ORDER BY id;

-- name: EnsureModule :exec
INSERT INTO modules (id, enabled, config, updated_at)
VALUES ($1, $2, $3, $4)
ON CONFLICT (id) DO NOTHING;

-- name: SetModuleEnabled :one
INSERT INTO modules (id, enabled, config, updated_at)
VALUES ($1, $2, '{}'::jsonb, $3)
ON CONFLICT (id) DO UPDATE
SET enabled = EXCLUDED.enabled,
    updated_at = EXCLUDED.updated_at
RETURNING *;

-- name: SetModuleConfig :one
UPDATE modules
SET config = $2,
    updated_at = $3
WHERE id = $1
RETURNING *;
