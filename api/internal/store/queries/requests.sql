-- SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
-- SPDX-License-Identifier: EUPL-1.2

-- name: GetRequestsSettings :one
SELECT * FROM requests_settings WHERE singleton;

-- name: UpsertRequestsSettings :one
INSERT INTO requests_settings (
    singleton,
    voting_mode,
    allow_viewer_votes,
    visibility,
    voting_ttl_days,
    updated_at
) VALUES (
    TRUE,
    $1,
    $2,
    $3,
    $4,
    $5
)
ON CONFLICT (singleton) DO UPDATE
SET voting_mode = EXCLUDED.voting_mode,
    allow_viewer_votes = EXCLUDED.allow_viewer_votes,
    visibility = EXCLUDED.visibility,
    voting_ttl_days = EXCLUDED.voting_ttl_days,
    updated_at = EXCLUDED.updated_at
RETURNING *;
