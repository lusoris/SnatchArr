-- SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
-- SPDX-License-Identifier: EUPL-1.2

-- name: GetSettings :one
SELECT * FROM settings WHERE singleton;

-- name: EnsureSettings :one
INSERT INTO settings (singleton, updated_at) VALUES (TRUE, $1)
ON CONFLICT (singleton) DO UPDATE SET singleton = TRUE
RETURNING *;

-- name: UpdateSettings :one
UPDATE settings
SET history_retention_days = $1, user_agent = $2, global_hourly_cap = $3, updated_at = $4
WHERE singleton
RETURNING *;
