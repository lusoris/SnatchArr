-- SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
-- SPDX-License-Identifier: EUPL-1.2

-- name: FilterUnprocessed :many
SELECT c.id::bigint AS entity_id
FROM unnest(sqlc.arg(entity_ids)::bigint[]) AS c(id)
WHERE NOT EXISTS (
    SELECT 1 FROM processed_items p
    WHERE p.instance_id = sqlc.arg(instance_id)
      AND p.kind = sqlc.arg(kind)
      AND p.entity_type = sqlc.arg(entity_type)
      AND p.entity_id = c.id
      AND p.expires_at > sqlc.arg(now)
);

-- name: MarkProcessed :exec
INSERT INTO processed_items (instance_id, kind, entity_type, entity_id, expires_at)
SELECT sqlc.arg(instance_id), sqlc.arg(kind), sqlc.arg(entity_type), c.id, sqlc.arg(expires_at)
FROM unnest(sqlc.arg(entity_ids)::bigint[]) AS c(id)
ON CONFLICT (instance_id, kind, entity_type, entity_id) DO UPDATE SET expires_at = EXCLUDED.expires_at;

-- name: CountProcessed :one
SELECT count(*) FROM processed_items WHERE instance_id = $1 AND expires_at > $2;

-- name: ResetProcessed :execrows
DELETE FROM processed_items
WHERE (sqlc.narg(instance_id)::uuid IS NULL OR instance_id = sqlc.narg(instance_id)::uuid);

-- name: PurgeExpiredProcessed :execrows
DELETE FROM processed_items WHERE expires_at <= $1;

-- name: EnsureBucket :exec
INSERT INTO rate_buckets (instance_id, window_start, used) VALUES ($1, $2, 0)
ON CONFLICT (instance_id, window_start) DO NOTHING;

-- name: LockBucket :one
SELECT used FROM rate_buckets WHERE instance_id = $1 AND window_start = $2 FOR UPDATE;

-- name: SetBucketUsed :exec
UPDATE rate_buckets SET used = $3 WHERE instance_id = $1 AND window_start = $2;

-- name: GetBucketUsed :one
SELECT COALESCE((SELECT used FROM rate_buckets WHERE instance_id = $1 AND window_start = $2), 0)::int AS used;

-- name: PurgeBucketsBefore :execrows
DELETE FROM rate_buckets WHERE window_start < $1;
