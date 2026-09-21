-- SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
-- SPDX-License-Identifier: EUPL-1.2

-- name: CreateSchedule :one
INSERT INTO schedules (id, instance_id, name, days_mask, start_time, end_time, tz, action, cap_value, enabled, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $11)
RETURNING *;

-- name: UpdateSchedule :one
UPDATE schedules
SET instance_id = $2, name = $3, days_mask = $4, start_time = $5, end_time = $6, tz = $7,
    action = $8, cap_value = $9, enabled = $10, updated_at = $11
WHERE id = $1
RETURNING *;

-- name: GetSchedule :one
SELECT * FROM schedules WHERE id = $1;

-- name: ListSchedules :many
SELECT * FROM schedules ORDER BY name;

-- name: ListEnabledSchedulesFor :many
SELECT * FROM schedules WHERE enabled AND (instance_id IS NULL OR instance_id = $1);

-- name: DeleteSchedule :execrows
DELETE FROM schedules WHERE id = $1;
