-- SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
-- SPDX-License-Identifier: EUPL-1.2

-- name: GetQueuePolicy :one
SELECT * FROM queue_policies
WHERE instance_id = $1;

-- name: UpsertQueuePolicy :exec
INSERT INTO queue_policies (
    instance_id, enabled, rules, grace_s, min_bytes_per_second,
    min_custom_format_score, max_removals_per_sweep, updated_at
) VALUES (
    $1, $2, $3, $4, $5,
    $6, $7, $8
)
ON CONFLICT (instance_id) DO UPDATE SET
    enabled = EXCLUDED.enabled,
    rules = EXCLUDED.rules,
    grace_s = EXCLUDED.grace_s,
    min_bytes_per_second = EXCLUDED.min_bytes_per_second,
    min_custom_format_score = EXCLUDED.min_custom_format_score,
    max_removals_per_sweep = EXCLUDED.max_removals_per_sweep,
    updated_at = EXCLUDED.updated_at;

-- name: ListQueueStrikes :many
SELECT * FROM queue_strikes
WHERE instance_id = $1
ORDER BY download_id;

-- name: UpsertQueueStrikes :exec
INSERT INTO queue_strikes (
    instance_id, download_id, queue_id, rule, count, last_size_left, last_seen, expires_at
)
SELECT
    sub.instance_id,
    sub.download_id,
    sub.queue_id,
    sub.rule,
    sub.count,
    sub.last_size_left,
    sub.last_seen,
    sub.last_seen + make_interval(secs => sqlc.arg(ttl_s)::bigint)
FROM (
    SELECT
        sqlc.arg(instance_id)::uuid AS instance_id,
        unnest(sqlc.arg(download_ids)::text[]) AS download_id,
        unnest(sqlc.arg(queue_ids)::int[]) AS queue_id,
        unnest(sqlc.arg(rules)::text[]) AS rule,
        unnest(sqlc.arg(counts)::int[]) AS count,
        unnest(sqlc.arg(last_size_lefts)::bigint[]) AS last_size_left,
        unnest(sqlc.arg(last_seens)::timestamptz[]) AS last_seen
) AS sub
ON CONFLICT (instance_id, download_id) DO UPDATE SET
    queue_id = EXCLUDED.queue_id,
    rule = EXCLUDED.rule,
    count = EXCLUDED.count,
    last_size_left = EXCLUDED.last_size_left,
    last_seen = EXCLUDED.last_seen,
    expires_at = EXCLUDED.expires_at;

-- name: DeleteQueueStrike :exec
DELETE FROM queue_strikes
WHERE instance_id = $1 AND download_id = $2;

-- name: PurgeExpiredQueueStrikes :execrows
-- The outer expires_at condition is repeated on purpose: Postgres re-checks it on a row a
-- concurrent sweep extended meanwhile, so that row is kept.
DELETE FROM queue_strikes
WHERE (instance_id, download_id) IN (
    SELECT instance_id, download_id FROM queue_strikes
    WHERE expires_at <= sqlc.arg(before)::timestamptz
    LIMIT sqlc.arg(batch)::int
) AND expires_at <= sqlc.arg(before)::timestamptz;
