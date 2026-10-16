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

-- name: RecentInstanceRunStatuses :many
SELECT status FROM snatch_runs
WHERE instance_id = $1 AND status IN ('done', 'failed')
ORDER BY finished_at DESC
LIMIT $2;

-- name: FindSeerrRequestForEntity :one
SELECT * FROM seerr_requests
WHERE instance_id = $1 AND entity_id = $2
LIMIT 1;
