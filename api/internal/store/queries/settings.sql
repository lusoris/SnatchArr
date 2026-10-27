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
SET history_retention_days = $1,
    user_agent = $2,
    global_hourly_cap = $3,
    search_priority_preset = $4,
    download_priority_preset = $5,
    llm_enabled = $6,
    llm_write_tools = $7,
    brand_mark = $8,
    sfw_mode = $9,
    updated_at = $10
WHERE singleton
RETURNING *;

-- name: GetUserPreferences :one
SELECT * FROM users_prefs WHERE user_id = $1;

-- name: UpsertUserPreferences :one
INSERT INTO users_prefs (user_id, brand_mark, sfw_mode, updated_at)
VALUES ($1, $2, $3, $4)
ON CONFLICT (user_id) DO UPDATE
SET brand_mark = EXCLUDED.brand_mark,
    sfw_mode = EXCLUDED.sfw_mode,
    updated_at = EXCLUDED.updated_at
RETURNING *;
