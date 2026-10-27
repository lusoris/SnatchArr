-- SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
-- SPDX-License-Identifier: EUPL-1.2

-- name: CreateNotificationChannel :one
INSERT INTO notification_channels (id, name, kind, enabled, config_enc, has_secret, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $7)
RETURNING *;

-- name: GetNotificationChannel :one
SELECT * FROM notification_channels WHERE id = $1;

-- name: ListNotificationChannels :many
SELECT * FROM notification_channels ORDER BY name;

-- name: ListEnabledNotificationChannels :many
SELECT * FROM notification_channels WHERE enabled ORDER BY name;

-- name: ListNotificationChannelsByIDs :many
SELECT * FROM notification_channels WHERE id = ANY(@ids::uuid[]) ORDER BY name;

-- name: UpdateNotificationChannel :one
UPDATE notification_channels
SET name = $2, kind = $3, enabled = $4, config_enc = $5, has_secret = $6, updated_at = $7
WHERE id = $1
RETURNING *;

-- name: DeleteNotificationChannel :execrows
DELETE FROM notification_channels WHERE id = $1;

-- name: CreateNotificationRule :one
INSERT INTO notification_rules (id, name, event_kind, threshold, enabled, channel_ids, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $7)
RETURNING *;

-- name: GetNotificationRule :one
SELECT * FROM notification_rules WHERE id = $1;

-- name: ListNotificationRules :many
SELECT * FROM notification_rules ORDER BY name;

-- name: ListEnabledNotificationRules :many
SELECT * FROM notification_rules WHERE enabled ORDER BY name;

-- name: ListNotificationRulesByEvent :many
SELECT * FROM notification_rules WHERE enabled AND event_kind = $1 ORDER BY name;

-- name: UpdateNotificationRule :one
UPDATE notification_rules
SET name = $2, event_kind = $3, threshold = $4, enabled = $5, channel_ids = $6, updated_at = $7
WHERE id = $1
RETURNING *;

-- name: DeleteNotificationRule :execrows
DELETE FROM notification_rules WHERE id = $1;

-- name: InstanceRunStatusesAsOf :many
SELECT r.status FROM snatch_runs r
CROSS JOIN (
    SELECT t.finished_at, t.id
    FROM snatch_runs t
    WHERE t.id = sqlc.arg(run_id) AND t.instance_id = sqlc.arg(instance_id)
) target
WHERE r.instance_id = sqlc.arg(instance_id)
  AND r.status IN ('done', 'failed')
  AND (r.finished_at, r.id) <= (target.finished_at, target.id)
ORDER BY r.finished_at DESC, r.id DESC
LIMIT sqlc.arg(limit_count);

-- name: FindSeerrRequestForEntity :one
SELECT * FROM seerr_requests
WHERE instance_id = $1 AND entity_id = $2
LIMIT 1;

-- name: CreateUserNotifyTarget :one
INSERT INTO user_notify_targets (id, user_id, kind, config_enc, enabled, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $6)
RETURNING *;

-- name: GetUserNotifyTarget :one
SELECT * FROM user_notify_targets
WHERE id = $1 AND user_id = $2;

-- name: ListUserNotifyTargets :many
SELECT * FROM user_notify_targets
WHERE user_id = $1
ORDER BY created_at ASC;

-- name: ListEnabledUserNotifyTargets :many
SELECT * FROM user_notify_targets
WHERE user_id = $1 AND enabled = TRUE
ORDER BY created_at ASC;

-- name: UpdateUserNotifyTarget :one
UPDATE user_notify_targets
SET kind = $3, config_enc = $4, enabled = $5, updated_at = $6
WHERE id = $1 AND user_id = $2
RETURNING *;

-- name: DeleteUserNotifyTarget :execrows
DELETE FROM user_notify_targets
WHERE id = $1 AND user_id = $2;

