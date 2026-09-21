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
-- Afterglow backoff: the first snatch rests base_s seconds, every further snatch of the
-- same item doubles the rest, capped at max_s.
INSERT INTO processed_items (instance_id, kind, entity_type, entity_id, expires_at, attempts, last_snatched_at)
SELECT sqlc.arg(instance_id), sqlc.arg(kind), sqlc.arg(entity_type), c.id,
       sqlc.arg(now)::timestamptz + make_interval(secs => sqlc.arg(base_s)::bigint), 1, sqlc.arg(now)::timestamptz
FROM unnest(sqlc.arg(entity_ids)::bigint[]) AS c(id)
ON CONFLICT (instance_id, kind, entity_type, entity_id) DO UPDATE SET
    attempts = processed_items.attempts + 1,
    last_snatched_at = EXCLUDED.last_snatched_at,
    expires_at = EXCLUDED.last_snatched_at + make_interval(
        secs => LEAST(sqlc.arg(base_s)::bigint * power(2, LEAST(processed_items.attempts, 20))::bigint, sqlc.arg(max_s)::bigint)
    );

-- name: ProcessedAttempts :one
SELECT COALESCE((
    SELECT attempts FROM processed_items
    WHERE instance_id = $1 AND kind = $2 AND entity_type = $3 AND entity_id = $4
), 0)::int AS attempts;

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

-- name: EnsureGlobalBucket :exec
INSERT INTO global_rate_buckets (window_start, used) VALUES ($1, 0)
ON CONFLICT (window_start) DO NOTHING;

-- name: LockGlobalBucket :one
SELECT used FROM global_rate_buckets WHERE window_start = $1 FOR UPDATE;

-- name: SetGlobalBucketUsed :exec
UPDATE global_rate_buckets SET used = $2 WHERE window_start = $1;

-- name: GetGlobalBucketUsed :one
SELECT COALESCE((SELECT used FROM global_rate_buckets WHERE window_start = $1), 0)::int AS used;

-- name: PurgeGlobalBucketsBefore :execrows
DELETE FROM global_rate_buckets WHERE window_start < $1;
