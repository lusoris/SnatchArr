-- SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
-- SPDX-License-Identifier: EUPL-1.2

-- name: EnqueueRun :one
INSERT INTO snatch_runs (id, instance_id, kind, status, queued_at, focus_entity_id, focus_group_id)
VALUES ($1, $2, $3, 'queued', $4, $5, $6)
RETURNING *;

-- name: HasActiveRun :one
SELECT EXISTS (
    SELECT 1 FROM snatch_runs
    WHERE instance_id = $1 AND kind = $2 AND status IN ('queued', 'leased')
);

-- name: LastFinishedAt :one
SELECT COALESCE(max(finished_at), '1970-01-01'::timestamptz)::timestamptz AS last_finished
FROM snatch_runs
WHERE instance_id = $1 AND kind = $2 AND status IN ('done', 'failed');

-- name: LeaseRun :one
UPDATE snatch_runs
SET status = 'leased', leased_by = $1, lease_expires_at = $2, started_at = COALESCE(started_at, $3)
WHERE id = (
    SELECT id FROM snatch_runs
    WHERE status = 'queued' OR (status = 'leased' AND lease_expires_at < $3)
    ORDER BY queued_at
    LIMIT 1
    FOR UPDATE SKIP LOCKED
)
RETURNING *;

-- name: GetRun :one
SELECT * FROM snatch_runs WHERE id = $1;

-- name: HeartbeatRun :one
UPDATE snatch_runs
SET lease_expires_at = $3
WHERE id = $1 AND leased_by = $2 AND status = 'leased'
RETURNING *;

-- name: CompleteRun :one
UPDATE snatch_runs
SET status = $3, finished_at = $4, searched_count = $5, error = $6, lease_expires_at = NULL
WHERE id = $1 AND leased_by = $2 AND status IN ('leased', 'cancelled')
RETURNING *;

-- name: CancelRun :execrows
UPDATE snatch_runs SET status = 'cancelled', finished_at = $2
WHERE id = $1 AND status IN ('queued', 'leased');

-- name: ListRuns :many
SELECT * FROM snatch_runs
WHERE (sqlc.narg(instance_id)::uuid IS NULL OR instance_id = sqlc.narg(instance_id)::uuid)
ORDER BY queued_at DESC
LIMIT $1 OFFSET $2;

-- name: RecentRunStatuses :many
SELECT status FROM snatch_runs
WHERE instance_id = $1 AND kind = $2 AND status IN ('done', 'failed')
ORDER BY finished_at DESC
LIMIT 10;

-- name: PurgeRunsBefore :execrows
DELETE FROM snatch_runs WHERE status IN ('done', 'failed', 'cancelled') AND finished_at < $1;
