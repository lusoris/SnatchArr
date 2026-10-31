-- SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
--
-- SPDX-License-Identifier: EUPL-1.2

-- name: ListPriorityPresets :many
SELECT * FROM priority_presets ORDER BY lower(name), id;

-- name: GetPriorityPreset :one
SELECT * FROM priority_presets WHERE id = $1;

-- name: CreatePriorityPreset :one
INSERT INTO priority_presets (id, name, search, download, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $5)
RETURNING *;

-- name: UpdatePriorityPreset :one
UPDATE priority_presets
SET name = $2, search = $3, download = $4, updated_at = $5
WHERE id = $1
RETURNING *;

-- name: DeletePriorityPreset :execrows
DELETE FROM priority_presets WHERE id = $1;

-- name: CountPriorityPresets :one
SELECT count(*) FROM priority_presets;

-- Serialize count and insert across API replicas so concurrent saves respect the cap.
-- name: LockPriorityPresets :exec
SELECT pg_advisory_xact_lock(hashtextextended('snatcharr.priority_presets', 0));
