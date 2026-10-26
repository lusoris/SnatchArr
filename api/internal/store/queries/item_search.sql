-- SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
-- SPDX-License-Identifier: EUPL-1.2

-- name: CheckAndRecordItemSearch :one
INSERT INTO item_search_cooldowns (user_id, last_searched_at)
VALUES (sqlc.arg(user_id), sqlc.arg(now)::timestamptz)
ON CONFLICT (user_id) DO UPDATE
SET last_searched_at = EXCLUDED.last_searched_at
WHERE item_search_cooldowns.last_searched_at <= sqlc.arg(now)::timestamptz - make_interval(secs => sqlc.arg(cooldown_seconds)::double precision)
RETURNING last_searched_at;

-- name: GetItemSearchCooldown :one
SELECT last_searched_at
FROM item_search_cooldowns
WHERE user_id = $1;

-- name: ResetItemSearchCooldown :exec
DELETE FROM item_search_cooldowns
WHERE user_id = $1;
