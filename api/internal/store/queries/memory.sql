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
-- The outer expires_at condition is repeated on purpose: Postgres re-checks it on a row a
-- concurrent snatch extended meanwhile, so that row is kept.
DELETE FROM processed_items
WHERE (instance_id, kind, entity_type, entity_id) IN (
    SELECT instance_id, kind, entity_type, entity_id FROM processed_items
    WHERE expires_at <= sqlc.arg(before)::timestamptz LIMIT sqlc.arg(batch)::int)
  AND expires_at <= sqlc.arg(before)::timestamptz;

-- Stamina is a rolling window of one-minute buckets (#16). Every grant for an instance
-- runs under this transaction-scoped lock, so two grants on either side of a minute
-- boundary (different bucket rows) still see each other.
-- name: LockInstanceBudget :exec
SELECT pg_advisory_xact_lock(hashtextextended('snatcharr.budget:' || sqlc.arg(instance_id)::uuid::text, 0));

-- name: BucketUsage :one
SELECT COALESCE(sum(used), 0)::int AS used,
       COALESCE(min(window_start), sqlc.arg(since)::timestamptz)::timestamptz AS oldest
FROM rate_buckets
WHERE instance_id = sqlc.arg(instance_id) AND window_start >= sqlc.arg(since)::timestamptz AND used > 0;

-- name: AddBucketUsed :exec
INSERT INTO rate_buckets (instance_id, window_start, used) VALUES (sqlc.arg(instance_id), sqlc.arg(window_start), sqlc.arg(used))
ON CONFLICT (instance_id, window_start) DO UPDATE SET used = rate_buckets.used + EXCLUDED.used;

-- name: PurgeBucketsBefore :execrows
DELETE FROM rate_buckets
WHERE (instance_id, window_start) IN (
    SELECT instance_id, window_start FROM rate_buckets
    WHERE window_start < sqlc.arg(before)::timestamptz LIMIT sqlc.arg(batch)::int);

-- Taken after LockInstanceBudget, never before, so the two locks cannot deadlock.
-- name: LockGlobalBudget :exec
SELECT pg_advisory_xact_lock(hashtextextended('snatcharr.budget:global', 0));

-- name: GlobalBucketUsage :one
SELECT COALESCE(sum(used), 0)::int AS used,
       COALESCE(min(window_start), sqlc.arg(since)::timestamptz)::timestamptz AS oldest
FROM global_rate_buckets
WHERE window_start >= sqlc.arg(since)::timestamptz AND used > 0;

-- name: AddGlobalBucketUsed :exec
INSERT INTO global_rate_buckets (window_start, used) VALUES (sqlc.arg(window_start), sqlc.arg(used))
ON CONFLICT (window_start) DO UPDATE SET used = global_rate_buckets.used + EXCLUDED.used;

-- name: PurgeGlobalBucketsBefore :execrows
DELETE FROM global_rate_buckets
WHERE window_start IN (
    SELECT window_start FROM global_rate_buckets
    WHERE window_start < sqlc.arg(before)::timestamptz LIMIT sqlc.arg(batch)::int);
