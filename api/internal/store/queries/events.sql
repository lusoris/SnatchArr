-- SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
-- SPDX-License-Identifier: EUPL-1.2

-- name: InsertEvent :one
INSERT INTO hunt_events (run_id, instance_id, ts, level, type, entity_type, entity_id, title, detail)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING id;

-- name: ListEvents :many
SELECT * FROM hunt_events
WHERE (sqlc.narg(instance_id)::uuid IS NULL OR instance_id = sqlc.narg(instance_id)::uuid)
  AND (sqlc.narg(before_id)::bigint IS NULL OR id < sqlc.narg(before_id)::bigint)
  AND (sqlc.narg(type)::text IS NULL OR type = sqlc.narg(type)::text)
ORDER BY id DESC
LIMIT sqlc.arg(page_size);

-- name: DeleteEventsForInstance :execrows
DELETE FROM hunt_events WHERE instance_id = $1;

-- name: PurgeEventsBefore :execrows
DELETE FROM hunt_events WHERE ts < $1;
