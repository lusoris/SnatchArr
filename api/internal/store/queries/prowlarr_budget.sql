-- SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
-- SPDX-License-Identifier: EUPL-1.2

-- Prowlarr link stamina is a rolling window of one-minute buckets (#777). Every grab for a link
-- runs under this transaction-scoped lock, so two grabs on either side of a minute
-- boundary still see each other.
-- Lock order: LockProwlarrLinkBudget first, then LockGlobalBudget.
-- name: LockProwlarrLinkBudget :exec
SELECT pg_advisory_xact_lock(hashtextextended('snatcharr.budget.prowlarr:' || sqlc.arg(link_id)::uuid::text, 0));

-- name: ProwlarrBucketUsage :one
SELECT COALESCE(sum(used), 0)::int AS used,
       COALESCE(min(window_start), sqlc.arg(since)::timestamptz)::timestamptz AS oldest
FROM prowlarr_rate_buckets
WHERE link_id = sqlc.arg(link_id) AND window_start >= sqlc.arg(since)::timestamptz AND used > 0;

-- name: AddProwlarrBucketUsed :exec
INSERT INTO prowlarr_rate_buckets (link_id, window_start, used) VALUES (sqlc.arg(link_id), sqlc.arg(window_start), sqlc.arg(used))
ON CONFLICT (link_id, window_start) DO UPDATE SET used = prowlarr_rate_buckets.used + EXCLUDED.used;

-- name: PurgeProwlarrBucketsBefore :execrows
DELETE FROM prowlarr_rate_buckets
WHERE (link_id, window_start) IN (
    SELECT link_id, window_start FROM prowlarr_rate_buckets
    WHERE window_start < sqlc.arg(before)::timestamptz LIMIT sqlc.arg(batch)::int);
